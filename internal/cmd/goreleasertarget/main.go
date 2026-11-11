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

// Command goreleasertarget prints a goreleaser configuration that builds for
// one operating system and architecture only.
//
// "goreleaser release", which makes the packages of a distribution, builds
// every target that its configuration lists. The open-source goreleaser can
// be told to build a single target only in "goreleaser build", which makes no
// packages: "goreleaser release --single-target" is a feature of its paid
// edition. So this command reads a configuration from its standard input and
// prints it with only the builds for one operating system and architecture
// left in it, each of them narrowed to that pair. With the result as its
// --config, "goreleaser release" builds and packages for that pair and for
// nothing else:
//
//	goreleasertarget -goos=linux -goarch=amd64 < .goreleaser.yaml > single-target.yaml
//
// The command understands builds that list what they are for under goos and
// goarch, as the configurations that distrogen generates do. It is an error
// if the goos or the goarch of a build is anything but a list of at least one
// plain value, or if a build has a targets list. Other settings of a build
// that affect its targets, such as goamd64 and ignore, are left as they are.
// So are YAML aliases: one that refers to an anchor inside a build that is
// dropped no longer refers to anything, and goreleaser rejects the result.
package main

import (
	"bytes"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"

	"go.yaml.in/yaml/v4"
)

func main() {
	goos := flag.String("goos", "", "operating system to keep the builds for, as GOOS names it (required)")
	goarch := flag.String("goarch", "", "architecture to keep the builds for, as GOARCH names it (required)")
	flag.Usage = func() {
		fmt.Fprintln(flag.CommandLine.Output(), "usage: goreleasertarget -goos=<os> -goarch=<arch> < .goreleaser.yaml > single-target.yaml")
		flag.PrintDefaults()
	}
	flag.Parse()
	if *goos == "" || *goarch == "" || flag.NArg() != 0 {
		flag.Usage()
		os.Exit(2)
	}
	if err := run(*goos, *goarch); err != nil {
		fmt.Fprintln(os.Stderr, "goreleasertarget:", err)
		os.Exit(1)
	}
}

// run reads a goreleaser configuration from the standard input and writes it
// to the standard output with only the builds for goos/goarch left in it.
func run(goos, goarch string) error {
	config, err := io.ReadAll(os.Stdin)
	if err != nil {
		return err
	}
	config, err = singleTarget(config, goos, goarch)
	if err != nil {
		return err
	}
	_, err = os.Stdout.Write(config)
	return err
}

// singleTarget returns the goreleaser configuration config with only the
// builds for goos/goarch left in it, each of them narrowed to that operating
// system and architecture. The rest of the configuration stays as it is.
//
// It is an error if no build is for goos/goarch: when its configuration lists
// no builds, goreleaser builds for its default targets.
func singleTarget(config []byte, goos, goarch string) ([]byte, error) {
	var document yaml.Node
	if err := yaml.Unmarshal(config, &document); err != nil {
		return nil, fmt.Errorf("could not parse the configuration: %w", err)
	}
	builds := documentBuilds(&document)
	if builds == nil {
		return nil, errors.New("the configuration has no list of builds")
	}

	var kept []*yaml.Node
	for i, build := range builds.Content {
		oses, arches := mappingScalarList(build, "goos"), mappingScalarList(build, "goarch")
		// goreleaser goes by the targets list of a build, if it has one, and
		// not by its goos and goarch. For a goos or a goarch that is missing
		// or empty, it goes by a default list.
		if oses == nil || arches == nil || mappingValue(build, "targets") != nil {
			return nil, fmt.Errorf("cannot narrow %s: its goos and its goarch have to be lists of at least one plain value, and it must not have a targets list", buildName(build, i))
		}
		targetOS, targetArch := findScalar(oses, goos), findScalar(arches, goarch)
		if targetOS == nil || targetArch == nil {
			continue
		}
		oses.Content = []*yaml.Node{targetOS}
		arches.Content = []*yaml.Node{targetArch}
		kept = append(kept, build)
	}
	if len(kept) == 0 {
		return nil, fmt.Errorf("no build in the configuration is for %s/%s", goos, goarch)
	}
	builds.Content = kept

	var output bytes.Buffer
	encoder := yaml.NewEncoder(&output)
	encoder.SetIndent(2)
	if err := encoder.Encode(&document); err != nil {
		return nil, err
	}
	if err := encoder.Close(); err != nil {
		return nil, err
	}
	return output.Bytes(), nil
}

// documentBuilds returns the list of builds of the goreleaser configuration
// that was parsed into document, or nil if it has none.
func documentBuilds(document *yaml.Node) *yaml.Node {
	// Parsing makes document a document node, whose only content is the root
	// of the configuration. It leaves document empty if there was nothing to
	// parse.
	if document.Kind != yaml.DocumentNode || len(document.Content) != 1 {
		return nil
	}
	builds := mappingValue(document.Content[0], "builds")
	if builds == nil || builds.Kind != yaml.SequenceNode {
		return nil
	}
	return builds
}

// mappingValue returns the value of key in mapping, or nil if mapping has no
// such key or is not a mapping.
func mappingValue(mapping *yaml.Node, key string) *yaml.Node {
	if mapping.Kind != yaml.MappingNode {
		return nil
	}
	// The content of a mapping node alternates between keys and values.
	for i := 0; i+1 < len(mapping.Content); i += 2 {
		if mapping.Content[i].Value == key {
			return mapping.Content[i+1]
		}
	}
	return nil
}

// mappingScalarList returns the value of key in mapping if it is a list of at
// least one plain value, and nil otherwise. An alias is not a plain value:
// what it stands for is written somewhere else in the document.
func mappingScalarList(mapping *yaml.Node, key string) *yaml.Node {
	list := mappingValue(mapping, key)
	if list == nil || list.Kind != yaml.SequenceNode || len(list.Content) == 0 {
		return nil
	}
	for _, element := range list.Content {
		if element.Kind != yaml.ScalarNode {
			return nil
		}
	}
	return list
}

// findScalar returns the element of list that is value, or nil if there is
// none. list has to be a list of plain values.
func findScalar(list *yaml.Node, value string) *yaml.Node {
	for _, element := range list.Content {
		if element.Value == value {
			return element
		}
	}
	return nil
}

// buildName names the build at index for an error message: by its id, or by
// its position if it has no id.
func buildName(build *yaml.Node, index int) string {
	if id := mappingValue(build, "id"); id != nil && id.Kind == yaml.ScalarNode {
		return fmt.Sprintf("build %q", id.Value)
	}
	return fmt.Sprintf("build %d", index+1)
}
