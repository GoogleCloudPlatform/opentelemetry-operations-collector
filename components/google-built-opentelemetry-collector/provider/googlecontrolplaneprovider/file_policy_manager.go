// Copyright 2026 Google LLC
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package googlecontrolplaneprovider

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"

	"github.com/GoogleCloudPlatform/opentelemetry-operations-collector/pkg/googlepolicy"
	"github.com/fsnotify/fsnotify"
	"go.uber.org/zap"
)

var (
	_ policyManager = (*filePolicyManager)(nil)

	// ErrPolicySetUnchanged indicates that the policy set has not changed.
	ErrPolicySetUnchanged = errors.New("policy set has not changed")
)

type filePolicyManager struct {
	logger      *zap.Logger
	originalURI *url.URL
	dir         string
	watcher     *fsnotify.Watcher
	done        chan struct{}
	wg          sync.WaitGroup
}

func NewFilePolicyManager(logger *zap.Logger, uri *url.URL) (*filePolicyManager, error) {
	// 1. Determine the directory we're looking at.
	// The file scheme expects a path to a directory on the current host.
	policyDir := uri.Path

	// EDGE CASE: When parsed out of a URL, Windows paths come with a leading `/`
	// i.e. "/C:/dir/on/my/machine".
	// Thanks NTFS for always being special. :)
	if len(policyDir) >= 3 && policyDir[0] == '/' && policyDir[2] == ':' {
		policyDir = policyDir[1:] // "C:/dir/on/machine"
	}

	// 2. Ensure the directory exists.
	fi, err := os.Stat(policyDir)
	if err != nil {
		return nil, fmt.Errorf("policy directory does not exist: %w", err)
	}
	if !fi.IsDir() {
		return nil, fmt.Errorf("policy path %q is not a directory", policyDir)
	}

	return &filePolicyManager{
		logger:      logger,
		originalURI: uri,
		dir:         policyDir,
	}, nil
}

func (fpm *filePolicyManager) Start() error {
	if fpm.watcher != nil {
		return errors.New("already started")
	}

	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		return fmt.Errorf("failed to create watcher: %w", err)
	}

	if err := watcher.Add(fpm.dir); err != nil {
		_ = watcher.Close()
		return fmt.Errorf("failed to watch directory %q: %w", fpm.dir, err)
	}

	if err := fpm.loadPolicySet(); err != nil && !errors.Is(err, ErrPolicySetUnchanged) {
		_ = watcher.Close()
		return fmt.Errorf("failed to load initial policy set: %w", err)
	}

	fpm.watcher = watcher
	fpm.done = make(chan struct{})

	fpm.wg.Add(1)
	go func() {
		defer fpm.wg.Done()
		for {
			select {
			case <-fpm.done:
				return
			case event, ok := <-watcher.Events:
				if !ok {
					return
				}
				if strings.EqualFold(filepath.Ext(event.Name), ".json") && (event.Has(fsnotify.Write) || event.Has(fsnotify.Create)) {
					_ = fpm.loadPolicySet()
				}
			case _, ok := <-watcher.Errors:
				if !ok {
					return
				}
			}
		}
	}()

	return nil
}

func (fpm *filePolicyManager) Stop() error {
	if fpm.done != nil {
		select {
		case <-fpm.done:
		default:
			close(fpm.done)
		}
	}
	var err error
	if fpm.watcher != nil {
		err = fpm.watcher.Close()
		fpm.watcher = nil
	}
	fpm.wg.Wait()
	return err
}

func (fpm *filePolicyManager) PolicyEvaluationResult(revisionID string, _ error) {
	// The file policy manager won't do anything with a failed policy evaluation.
	// Other managers may opt to send a response back to the original communication
	// source, but we have no one to tell. :)

	// TODO: What we may do is log some events here.
}

func (fpm *filePolicyManager) URI() *url.URL {
	return fpm.originalURI
}

func (fpm *filePolicyManager) loadPolicySet() error {
	// 0. Load the active policy set from `pkg/googlepolicy`. (may be nil)
	activePolicySet := googlepolicy.ActivePolicySet()

	// 1. Read all policy files in directory by assuming all json files found in the directory are policies.
	// Add them to a map of filename to readers so that the bytes of each file can be read.
	entries, err := os.ReadDir(fpm.dir)
	if err != nil {
		return fmt.Errorf("failed to read policy directory %q: %w", fpm.dir, err)
	}

	readers := make(map[string]*bytes.Reader)
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		if !strings.EqualFold(filepath.Ext(entry.Name()), ".json") {
			continue
		}

		filePath := filepath.Join(fpm.dir, entry.Name())
		data, err := os.ReadFile(filePath)
		if err != nil {
			return fmt.Errorf("failed to read policy file %q: %w", filePath, err)
		}
		readers[entry.Name()] = bytes.NewReader(data)
	}

	// 2. Calculate a revision ID by hashing the contents of all JSON files combined.
	// If the revision ID hash matches the active policy set's, return a harmless error that makes clear
	// the policy set hasn't changed.
	filenames := make([]string, 0, len(readers))
	for name := range readers {
		filenames = append(filenames, name)
	}
	slices.Sort(filenames)

	hasher := sha256.New()
	for _, name := range filenames {
		r := readers[name]
		if _, err := io.Copy(hasher, r); err != nil {
			return fmt.Errorf("failed to hash policy file %q: %w", name, err)
		}
		if _, err := r.Seek(0, io.SeekStart); err != nil {
			return fmt.Errorf("failed to rewind reader for %q: %w", name, err)
		}
	}
	revisionID := hex.EncodeToString(hasher.Sum(nil))

	if activePolicySet != nil && activePolicySet.RevisionID == revisionID {
		return ErrPolicySetUnchanged
	}

	// 3. Create a new policy set by unmarshalling each JSON file into `map[string]any` individually and
	// passing them all to `pkg/googlepolicy.MakePolicySet` using the calculated revision ID from the previous step.
	// (If a file can't be successfully unmarshalled, skip it and Debug log the error)
	rawPolicies := make([]map[string]any, 0, len(filenames))
	for _, name := range filenames {
		r := readers[name]
		var rawPolicy map[string]any
		if err := json.NewDecoder(r).Decode(&rawPolicy); err != nil {
			if fpm.logger != nil {
				fpm.logger.Debug("failed to unmarshal policy file", zap.String("file", name), zap.Error(err))
			}
			continue
		}
		if rawPolicy == nil {
			if fpm.logger != nil {
				fpm.logger.Debug("policy file does not contain a JSON object", zap.String("file", name))
			}
			continue
		}
		rawPolicies = append(rawPolicies, rawPolicy)
	}

	policySet, err := googlepolicy.MakePolicySet(revisionID, rawPolicies)
	if err != nil {
		return fmt.Errorf("failed to make policy set: %w", err)
	}

	// 4. If the policy set was successfully constructed, register it as the new active policy set in `pkg/googlepolicy`.
	googlepolicy.SetActivePolicySet(policySet)
	return nil
}
