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

package filelogsource

import (
	"context"
	"testing"

	policyv1alpha1 "github.com/GoogleCloudPlatform/opentelemetry-operations-collector/gen/go/policy/v1alpha1"
	"github.com/GoogleCloudPlatform/opentelemetry-operations-collector/pkg/googlepolicy"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/collector/component"
	"google.golang.org/protobuf/proto"
)

func TestPolicyValidation_ValidLinuxPaths(t *testing.T) {
	validPaths := [][]string{
		{"/var/log/**/*.log"},
		{"/var/log/messages"},
		{"/var/log/**"},
		{"/opt/google/logs/*.log"},
		{"/srv/web/logs/**/*.log"},
		{"/var/log/syslog", "/opt/app/logs/server.log"},
		{"/var/log/app.log", "/var/log/App.log"},
	}

	for _, paths := range validPaths {
		t.Run(paths[0], func(t *testing.T) {
			pb := &policyv1alpha1.FileLogSourcePolicy{
				Id:            "valid-policy",
				IncludedPaths: paths,
			}
			policy, err := NewPolicyFromProto(pb)
			require.NoError(t, err)
			require.NotNil(t, policy)
			assert.NoError(t, policy.Validate())
		})
	}
}

func TestPolicyValidation_ValidWindowsPaths(t *testing.T) {
	validPaths := [][]string{
		{`C:\ProgramData\MyApp\logs\app.log`},
		{`C:/ProgramData/MyApp/logs/app.log`},
		{`%ProgramData%\MyApp\logs\*.log`},
		{`D:\ProgramData\MyApp\logs\*.log`},
		{`C:\inetpub\logs\LogFiles\**\*.log`},
		{`C:/inetpub/logs/LogFiles/**/*.log`},
		{`%SystemDrive%\inetpub\logs\*.log`},
	}

	for _, paths := range validPaths {
		t.Run(paths[0], func(t *testing.T) {
			pb := &policyv1alpha1.FileLogSourcePolicy{
				Id:            "windows-policy",
				IncludedPaths: paths,
			}
			policy, err := NewPolicyFromProto(pb)
			require.NoError(t, err)
			require.NotNil(t, policy)
			assert.NoError(t, policy.Validate())
		})
	}
}

func TestPolicyValidation_InvalidPaths(t *testing.T) {
	testCases := []struct {
		name        string
		id          string
		include     []string
		expectedErr error
	}{
		{
			name:        "empty id",
			id:          "",
			include:     []string{"/var/log/app.log"},
			expectedErr: ErrMissingID,
		},
		{
			name:        "empty include slice",
			id:          "test-policy",
			include:     []string{},
			expectedErr: ErrMissingInclude,
		},
		{
			name:        "empty include pattern",
			id:          "test-policy",
			include:     []string{""},
			expectedErr: ErrEmptyIncludePattern,
		},
		{
			name:        "path traversal linux",
			id:          "test-policy",
			include:     []string{"/var/log/../../etc/shadow"},
			expectedErr: ErrPathTraversal,
		},
		{
			name:        "path traversal windows",
			id:          "test-policy",
			include:     []string{`C:\ProgramData\logs\..\..\Windows\System32`},
			expectedErr: ErrPathTraversal,
		},
		{
			name:        "sensitive etc",
			id:          "test-policy",
			include:     []string{"/etc/shadow"},
			expectedErr: ErrSensitiveDirectory,
		},
		{
			name:        "sensitive root",
			id:          "test-policy",
			include:     []string{"/root/.ssh/id_rsa"},
			expectedErr: ErrSensitiveDirectory,
		},
		{
			name:        "sensitive proc",
			id:          "test-policy",
			include:     []string{"/proc/cpuinfo"},
			expectedErr: ErrSensitiveDirectory,
		},
		{
			name:        "sensitive sys",
			id:          "test-policy",
			include:     []string{"/sys/class/net"},
			expectedErr: ErrSensitiveDirectory,
		},
		{
			name:        "sensitive var run",
			id:          "test-policy",
			include:     []string{"/var/run/secrets/kubernetes.io/serviceaccount/token"},
			expectedErr: ErrSensitiveDirectory,
		},
		{
			name:        "sensitive dev",
			id:          "test-policy",
			include:     []string{"/dev/null"},
			expectedErr: ErrSensitiveDirectory,
		},
		{
			name:        "sensitive bin",
			id:          "test-policy",
			include:     []string{"/bin/sh"},
			expectedErr: ErrSensitiveDirectory,
		},
		{
			name:        "disallowed home directory",
			id:          "test-policy",
			include:     []string{"/home/user/app.log"},
			expectedErr: ErrDisallowedLogRoot,
		},
		{
			name:        "disallowed tmp directory",
			id:          "test-policy",
			include:     []string{"/tmp/app.log"},
			expectedErr: ErrDisallowedLogRoot,
		},
		{
			name:        "disallowed windows system32",
			id:          "test-policy",
			include:     []string{`C:\Windows\System32\cmd.exe`},
			expectedErr: ErrDisallowedLogRoot,
		},
		{
			name:        "disallowed windows systemroot inetpub",
			id:          "test-policy",
			include:     []string{`%SystemRoot%\inetpub\logs\*.log`},
			expectedErr: ErrDisallowedLogRoot,
		},
		{
			name:        "relative path linux",
			id:          "test-policy",
			include:     []string{"var/log/app.log"},
			expectedErr: ErrRelativePath,
		},
		{
			name:        "relative path windows",
			id:          "test-policy",
			include:     []string{`ProgramData\MyApp\logs\app.log`},
			expectedErr: ErrRelativePath,
		},
		{
			name:        "duplicate path linux",
			id:          "test-policy",
			include:     []string{"/var/log/app.log", "/var/log/app.log"},
			expectedErr: ErrDuplicateIncludePattern,
		},
		{
			name:        "duplicate path windows case-insensitive",
			id:          "test-policy",
			include:     []string{`C:\ProgramData\MyApp\logs\app.log`, `c:\programdata\myapp\logs\app.log`},
			expectedErr: ErrDuplicateIncludePattern,
		},
		{
			name:        "duplicate path windows separator normalized",
			id:          "test-policy",
			include:     []string{`C:\inetpub\logs\app.log`, `C:/inetpub/logs/app.log`},
			expectedErr: ErrDuplicateIncludePattern,
		},
		{
			name:        "duplicate path windows env var expanded",
			id:          "test-policy",
			include:     []string{`%ProgramData%\MyApp\logs\app.log`, `C:\ProgramData\MyApp\logs\app.log`},
			expectedErr: ErrDuplicateIncludePattern,
		},
		{
			name:        "duplicate path windows systemdrive expanded",
			id:          "test-policy",
			include:     []string{`%SystemDrive%\inetpub\logs\*.log`, `C:\inetpub\logs\*.log`},
			expectedErr: ErrDuplicateIncludePattern,
		},
		{
			name:        "invalid glob unclosed bracket",
			id:          "test-policy",
			include:     []string{"/var/log/[a-z"},
			expectedErr: ErrInvalidGlobPattern,
		},
		{
			name:        "invalid glob unclosed brace",
			id:          "test-policy",
			include:     []string{"/var/log/{app,web"},
			expectedErr: ErrInvalidGlobPattern,
		},
		{
			name:        "invalid glob unmatched closing brace",
			id:          "test-policy",
			include:     []string{"/var/log/}app"},
			expectedErr: ErrInvalidGlobPattern,
		},
		{
			name:        "invalid glob windows unclosed bracket",
			id:          "test-policy",
			include:     []string{`C:\ProgramData\MyApp\logs\[a-z`},
			expectedErr: ErrInvalidGlobPattern,
		},
		{
			name:        "bare directory /var/log",
			id:          "test-policy",
			include:     []string{"/var/log"},
			expectedErr: ErrBareDirectoryPath,
		},
		{
			name:        "bare directory /var/log/",
			id:          "test-policy",
			include:     []string{"/var/log/"},
			expectedErr: ErrBareDirectoryPath,
		},
		{
			name:        "bare directory /opt/google/logs",
			id:          "test-policy",
			include:     []string{"/opt/google/logs"},
			expectedErr: ErrBareDirectoryPath,
		},
		{
			name:        "bare directory /opt/google/logs/",
			id:          "test-policy",
			include:     []string{"/opt/google/logs/"},
			expectedErr: ErrBareDirectoryPath,
		},
		{
			name:        "bare directory /srv/web/logs",
			id:          "test-policy",
			include:     []string{"/srv/web/logs"},
			expectedErr: ErrBareDirectoryPath,
		},
		{
			name:        "bare directory /srv/web/logs/",
			id:          "test-policy",
			include:     []string{"/srv/web/logs/"},
			expectedErr: ErrBareDirectoryPath,
		},
		{
			name:        "bare directory windows ProgramData logs",
			id:          "test-policy",
			include:     []string{`C:\ProgramData\MyApp\logs`},
			expectedErr: ErrBareDirectoryPath,
		},
		{
			name:        "bare directory windows ProgramData logs trailing backslash",
			id:          "test-policy",
			include:     []string{`C:\ProgramData\MyApp\logs\`},
			expectedErr: ErrBareDirectoryPath,
		},
		{
			name:        "bare directory windows env ProgramData logs",
			id:          "test-policy",
			include:     []string{`%ProgramData%\MyApp\logs`},
			expectedErr: ErrBareDirectoryPath,
		},
		{
			name:        "bare directory windows inetpub logs",
			id:          "test-policy",
			include:     []string{`C:\inetpub\logs`},
			expectedErr: ErrBareDirectoryPath,
		},
		{
			name:        "bare directory windows inetpub logs trailing slash",
			id:          "test-policy",
			include:     []string{`C:\inetpub\logs\`},
			expectedErr: ErrBareDirectoryPath,
		},
		{
			name:        "bare directory windows SystemDrive inetpub logs",
			id:          "test-policy",
			include:     []string{`%SystemDrive%\inetpub\logs`},
			expectedErr: ErrBareDirectoryPath,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			pb := &policyv1alpha1.FileLogSourcePolicy{
				Id:            tc.id,
				IncludedPaths: tc.include,
			}
			policy, err := NewPolicyFromProto(pb)
			require.Error(t, err)
			assert.Nil(t, policy)
			assert.ErrorIs(t, err, tc.expectedErr)
		})
	}
}

func TestPolicy_Metadata(t *testing.T) {
	pb := &policyv1alpha1.FileLogSourcePolicy{
		Id:            "test-log-source",
		IncludedPaths: []string{"/var/log/test.log"},
	}
	p, err := NewPolicyFromProto(pb)
	require.NoError(t, err)

	assert.Equal(t, "test-log-source", p.PolicyName())
	assert.Equal(t, PolicyType, p.PolicyType())
	assert.Equal(t, googlepolicy.PolicyClassSource, p.PolicyClass())
	assert.Equal(t, []string{"/var/log/test.log"}, p.IncludedPaths())

	// Exclude patterns and immutability check.
	assert.Equal(t, []string{
		"**/*.gz",
		"**/*.bz2",
		"**/*.xz",
		"**/*.zst",
		"**/*.zip",
	}, DefaultExcludePatterns())
	patterns := DefaultExcludePatterns()
	patterns[0] = "mutated"
	assert.NotEqual(t, patterns[0], DefaultExcludePatterns()[0])

	// Defensive zero-value checks.
	empty := &Policy{}
	assert.Equal(t, "", empty.PolicyName())
	assert.Nil(t, empty.IncludedPaths())
	assert.ErrorIs(t, empty.Validate(), ErrNilProto)
	_, err = NewPolicyFromProto(nil)
	assert.ErrorIs(t, err, ErrNilProto)
}

func TestPolicy_Evaluate(t *testing.T) {
	pb := &policyv1alpha1.FileLogSourcePolicy{
		Id:            "projects/123/locations/global/policySets/ps1/policies/app-logs",
		IncludedPaths: []string{"/var/log/app/*.log"},
	}
	policy, err := NewPolicyFromProto(pb)
	require.NoError(t, err)

	conf, err := policy.Evaluate(context.Background())
	require.NoError(t, err)
	require.NotNil(t, conf)

	expectedKey := "receivers::filelog"
	require.True(t, conf.IsSet(expectedKey))

	sub, err := conf.Sub(expectedKey)
	require.NoError(t, err)
	cfgMap := sub.ToStringMap()

	assert.Equal(t, []string{"/var/log/app/*.log"}, cfgMap["include"])
	assert.Equal(t, []string{
		"**/*.gz",
		"**/*.bz2",
		"**/*.xz",
		"**/*.zst",
		"**/*.zip",
	}, cfgMap["exclude"])
	assert.Equal(t, true, cfgMap["include_file_path"])

	// Validate error on invalid internal proto.
	invalidPolicy := &Policy{
		proto: &policyv1alpha1.FileLogSourcePolicy{
			Id: "",
		},
	}
	_, err = invalidPolicy.Evaluate(context.Background())
	assert.Error(t, err)
}

func TestPolicy_Evaluate_WindowsEnvExpansion(t *testing.T) {
	pb := &policyv1alpha1.FileLogSourcePolicy{
		Id: "win-env-logs",
		IncludedPaths: []string{
			`%ProgramData%\MyApp\logs\*.log`,
			`%SystemDrive%\inetpub\logs\*.log`,
		},
	}
	policy, err := NewPolicyFromProto(pb)
	require.NoError(t, err)

	conf, err := policy.Evaluate(context.Background())
	require.NoError(t, err)
	require.NotNil(t, conf)

	sub, err := conf.Sub("receivers::filelog")
	require.NoError(t, err)
	cfgMap := sub.ToStringMap()

	assert.Equal(t, []string{
		`C:\ProgramData\MyApp\logs\*.log`,
		`C:\inetpub\logs\*.log`,
	}, cfgMap["include"])
}

func TestPolicy_ReceiverIDs(t *testing.T) {
	pb := &policyv1alpha1.FileLogSourcePolicy{
		Id:            "nginx-logs",
		IncludedPaths: []string{"/var/log/nginx/*.log"},
	}
	policy, err := NewPolicyFromProto(pb)
	require.NoError(t, err)

	assert.Equal(t, []component.ID{component.MustNewID(DefaultReceiverID)}, policy.LogReceiverIDs())
	assert.Nil(t, policy.MetricReceiverIDs())
	assert.Nil(t, policy.TraceReceiverIDs())
}

func TestMerge_NilAndEmpty(t *testing.T) {
	merged, err := Merge()
	require.NoError(t, err)
	assert.Nil(t, merged)

	merged, err = Merge([]*Policy{}...)
	require.NoError(t, err)
	assert.Nil(t, merged)

	merged, err = Merge(nil)
	assert.ErrorIs(t, err, ErrNilProto)
	assert.Nil(t, merged)
}

func TestMerge_SinglePolicy(t *testing.T) {
	p, err := NewPolicyFromProto(&policyv1alpha1.FileLogSourcePolicy{
		Id:            "single-policy",
		IncludedPaths: []string{"/var/log/single/*.log"},
	})
	require.NoError(t, err)

	merged, err := Merge(p)
	require.NoError(t, err)
	assert.Equal(t, p, merged)
	assert.Equal(t, "single-policy", merged.PolicyName())
	assert.Equal(t, []string{"/var/log/single/*.log"}, merged.IncludedPaths())
}

func TestMerge_MultiplePolicies_DeduplicatedAndSorted(t *testing.T) {
	p1, err := NewPolicyFromProto(&policyv1alpha1.FileLogSourcePolicy{
		Id:            "policy-1",
		IncludedPaths: []string{"/var/log/syslog", "/var/log/nginx/*.log"},
	})
	require.NoError(t, err)

	p2, err := NewPolicyFromProto(&policyv1alpha1.FileLogSourcePolicy{
		Id:            "policy-2",
		IncludedPaths: []string{"/var/log/nginx/*.log", "/var/log/auth.log"},
	})
	require.NoError(t, err)

	p3, err := NewPolicyFromProto(&policyv1alpha1.FileLogSourcePolicy{
		Id:            "policy-3",
		IncludedPaths: []string{"/opt/app/logs/*.log", "/var/log/syslog"},
	})
	require.NoError(t, err)

	merged, err := Merge(p1, p2, p3)
	require.NoError(t, err)
	require.NotNil(t, merged)

	assert.Equal(t, "filelog_consolidated", merged.PolicyName())
	assert.Equal(t, []string{
		"/opt/app/logs/*.log",
		"/var/log/auth.log",
		"/var/log/nginx/*.log",
		"/var/log/syslog",
	}, merged.IncludedPaths())

	// Verify merged policy evaluation uses DefaultReceiverID.
	conf, err := merged.Evaluate(context.Background())
	require.NoError(t, err)
	assert.True(t, conf.IsSet("receivers::filelog"))

	sub, err := conf.Sub("receivers::filelog")
	require.NoError(t, err)
	assert.Equal(t, []string{
		"/opt/app/logs/*.log",
		"/var/log/auth.log",
		"/var/log/nginx/*.log",
		"/var/log/syslog",
	}, sub.ToStringMap()["include"])
}

func TestMerge_MultiplePolicies_WindowsCaseInsensitiveDeduplication(t *testing.T) {
	p1, err := NewPolicyFromProto(&policyv1alpha1.FileLogSourcePolicy{
		Id:            "win-1",
		IncludedPaths: []string{`C:\ProgramData\App\logs\app.log`},
	})
	require.NoError(t, err)

	p2, err := NewPolicyFromProto(&policyv1alpha1.FileLogSourcePolicy{
		Id:            "win-2",
		IncludedPaths: []string{`c:/programdata/app/logs/app.log`},
	})
	require.NoError(t, err)

	merged, err := Merge(p1, p2)
	require.NoError(t, err)
	require.NotNil(t, merged)
	assert.Len(t, merged.IncludedPaths(), 1)
	assert.Equal(t, `C:\ProgramData\App\logs\app.log`, merged.IncludedPaths()[0])
}

func TestMerge_MultiplePolicies_WindowsEnvDeduplication(t *testing.T) {
	p1, err := NewPolicyFromProto(&policyv1alpha1.FileLogSourcePolicy{
		Id:            "win-env-1",
		IncludedPaths: []string{`%ProgramData%\App\logs\app.log`},
	})
	require.NoError(t, err)

	p2, err := NewPolicyFromProto(&policyv1alpha1.FileLogSourcePolicy{
		Id:            "win-env-2",
		IncludedPaths: []string{`C:\ProgramData\App\logs\app.log`},
	})
	require.NoError(t, err)

	merged, err := Merge(p1, p2)
	require.NoError(t, err)
	require.NotNil(t, merged)
	assert.Len(t, merged.IncludedPaths(), 1)
	assert.Equal(t, `C:\ProgramData\App\logs\app.log`, merged.IncludedPaths()[0])

	// Reverse merge order must produce the identical result.
	mergedRev, err := Merge(p2, p1)
	require.NoError(t, err)
	require.NotNil(t, mergedRev)
	assert.Equal(t, merged.IncludedPaths(), mergedRev.IncludedPaths())

	// Evaluating the merged policy should produce expanded path.
	conf, err := merged.Evaluate(context.Background())
	require.NoError(t, err)
	sub, err := conf.Sub("receivers::filelog")
	require.NoError(t, err)
	assert.Equal(t, []string{`C:\ProgramData\App\logs\app.log`}, sub.ToStringMap()["include"])
}

func TestMerge_MultiplePolicies_WindowsEnvSortingDeterminism(t *testing.T) {
	p1, err := NewPolicyFromProto(&policyv1alpha1.FileLogSourcePolicy{
		Id:            "win-sort-1",
		IncludedPaths: []string{`%ProgramData%\App\logs\z.log`},
	})
	require.NoError(t, err)

	p2, err := NewPolicyFromProto(&policyv1alpha1.FileLogSourcePolicy{
		Id:            "win-sort-2",
		IncludedPaths: []string{`C:\ProgramData\App\logs\a.log`},
	})
	require.NoError(t, err)

	expected := []string{
		`C:\ProgramData\App\logs\a.log`,
		`C:\ProgramData\App\logs\z.log`,
	}

	merged1, err := Merge(p1, p2)
	require.NoError(t, err)
	assert.Equal(t, expected, merged1.IncludedPaths())

	merged2, err := Merge(p2, p1)
	require.NoError(t, err)
	assert.Equal(t, expected, merged2.IncludedPaths())
}

func TestMerge_FailsFastOnInvalidPolicy(t *testing.T) {
	valid, err := NewPolicyFromProto(&policyv1alpha1.FileLogSourcePolicy{
		Id:            "valid",
		IncludedPaths: []string{"/var/log/valid.log"},
	})
	require.NoError(t, err)

	invalid := &Policy{
		proto: &policyv1alpha1.FileLogSourcePolicy{
			Id:            "invalid",
			IncludedPaths: []string{"/etc/shadow"},
		},
	}

	merged, err := Merge(valid, invalid)
	assert.ErrorIs(t, err, ErrSensitiveDirectory)
	assert.Nil(t, merged)

	merged, err = Merge(valid, nil)
	assert.ErrorIs(t, err, ErrNilProto)
	assert.Nil(t, merged)
}

func TestConsolidate(t *testing.T) {
	p1, err := NewPolicyFromProto(&policyv1alpha1.FileLogSourcePolicy{
		Id:            "p1",
		IncludedPaths: []string{"/var/log/a.log", "/var/log/shared.log"},
	})
	require.NoError(t, err)

	p2, err := NewPolicyFromProto(&policyv1alpha1.FileLogSourcePolicy{
		Id:            "p2",
		IncludedPaths: []string{"/var/log/b.log", "/var/log/shared.log"},
	})
	require.NoError(t, err)

	p3, err := NewPolicyFromProto(&policyv1alpha1.FileLogSourcePolicy{
		Id:            "p3",
		IncludedPaths: []string{"/opt/app/logs/*.log"},
	})
	require.NoError(t, err)

	// Consolidate empty
	merged, err := p1.Consolidate(nil)
	require.NoError(t, err)
	assert.Nil(t, merged)

	// Consolidate single
	merged, err = p1.Consolidate([]googlepolicy.SourcePolicy{p1})
	require.NoError(t, err)
	assert.Equal(t, p1, merged)

	// Consolidate multiple with deduplication and set-union merging
	merged, err = p1.Consolidate([]googlepolicy.SourcePolicy{p1, p2, p3})
	require.NoError(t, err)
	require.NotNil(t, merged)
	assert.Equal(t, "filelog_consolidated", merged.PolicyName())
	assert.Equal(t, []string{
		"/opt/app/logs/*.log",
		"/var/log/a.log",
		"/var/log/b.log",
		"/var/log/shared.log",
	}, merged.(interface{ IncludedPaths() []string }).IncludedPaths())
}

func TestDriver_WireProtoAndMapLoading(t *testing.T) {
	pb := &policyv1alpha1.FileLogSourcePolicy{
		Id:            "wire-log-source",
		IncludedPaths: []string{"/var/log/**/*.log"},
	}

	// 1. Loading via MakePolicySetFromProtos (wire proto delivery path).
	psFromProto, err := googlepolicy.MakePolicySetFromProtos("test-policy-set", "rev1", []proto.Message{pb})
	require.NoError(t, err)
	require.Len(t, psFromProto.Policies, 1)

	entry := psFromProto.Policies["wire-log-source"]
	require.NotNil(t, entry)
	assert.Equal(t, PolicyType, entry.PolicyObj.PolicyType())
	assert.Equal(t, googlepolicy.PolicyClassSource, entry.PolicyObj.PolicyClass())

	// 2. Loading via MakePolicySet (authored map delivery path).
	rawConfig := []map[string]any{
		{
			"type":           PolicyType,
			"id":             "authored-log-source",
			"included_paths": []any{"/opt/services/logs/*.log"},
		},
	}
	psFromMap, err := googlepolicy.MakePolicySet("rev2", rawConfig)
	require.NoError(t, err)
	require.Len(t, psFromMap.Policies, 1)

	mapEntry := psFromMap.Policies["authored-log-source"]
	require.NotNil(t, mapEntry)
	assert.Equal(t, PolicyType, mapEntry.PolicyObj.PolicyType())
	assert.Equal(t, googlepolicy.PolicyClassSource, mapEntry.PolicyObj.PolicyClass())
	srcPolicy, ok := mapEntry.PolicyObj.(*Policy)
	require.True(t, ok)
	assert.Equal(t, []string{"/opt/services/logs/*.log"}, srcPolicy.IncludedPaths())
}

func TestPolicyValidation_TestAllowedRoot(t *testing.T) {
	// Verify that testing.Testing() returns true in this test execution context.
	require.True(t, testing.Testing(), "testing.Testing() must return true under go test")

	tempDir := t.TempDir()

	// 1. Without FILELOG_TEST_ALLOWED_ROOT, paths in tempDir are rejected.
	t.Setenv("FILELOG_TEST_ALLOWED_ROOT", "")
	pbUnset := &policyv1alpha1.FileLogSourcePolicy{
		Id:            "temp-dir-unset-policy",
		IncludedPaths: []string{tempDir + "/*.log"},
	}
	_, err := NewPolicyFromProto(pbUnset)
	require.ErrorIs(t, err, ErrDisallowedLogRoot)

	// 2. With FILELOG_TEST_ALLOWED_ROOT set, paths within tempDir are accepted.
	t.Setenv("FILELOG_TEST_ALLOWED_ROOT", tempDir)
	pbSet := &policyv1alpha1.FileLogSourcePolicy{
		Id:            "temp-dir-policy",
		IncludedPaths: []string{tempDir + "/*.log"},
	}
	polSet, err := NewPolicyFromProto(pbSet)
	require.NoError(t, err)
	assert.NoError(t, polSet.Validate())

	// 3. Sensitive directories are strictly rejected even when FILELOG_TEST_ALLOWED_ROOT is set.
	pbSensitive := &policyv1alpha1.FileLogSourcePolicy{
		Id:            "sensitive-policy",
		IncludedPaths: []string{"/etc/shadow"},
	}
	_, err = NewPolicyFromProto(pbSensitive)
	require.ErrorIs(t, err, ErrSensitiveDirectory)

	// 4. Path traversal escaping the root is strictly rejected.
	pbTraversal := &policyv1alpha1.FileLogSourcePolicy{
		Id:            "traversal-policy",
		IncludedPaths: []string{tempDir + "/../etc/shadow"},
	}
	_, err = NewPolicyFromProto(pbTraversal)
	require.ErrorIs(t, err, ErrPathTraversal)

	// 5. Windows path matching under FILELOG_TEST_ALLOWED_ROOT (case-insensitive and separator-normalized).
	t.Setenv("FILELOG_TEST_ALLOWED_ROOT", "C:\\TestRoot\\logs")
	pbWin := &policyv1alpha1.FileLogSourcePolicy{
		Id:            "win-test-policy",
		IncludedPaths: []string{"c:/testroot/logs/app.log", "C:\\TestRoot\\logs\\sub\\*.log"},
	}
	polWin, err := NewPolicyFromProto(pbWin)
	require.NoError(t, err)
	assert.NoError(t, polWin.Validate())
}

func FuzzPolicyValidation(f *testing.F) {
	seeds := []string{
		"/var/log/**/*.log",
		"/var/log/syslog",
		"/var/log",
		"/etc/shadow",
		"/tmp/test.log",
		`C:\ProgramData\App\logs\*.log`,
		`%ProgramData%\App\logs\*.log`,
		`C:\inetpub\logs\*.log`,
		`C:\inetpub\logs`,
		`C:\Windows\System32\app.log`,
		"/var/log/../etc/passwd",
		"relative/path/test.log",
		"/var/log/[a-z",
		"/var/log/{a,b",
	}
	for _, seed := range seeds {
		f.Add(seed)
	}

	f.Fuzz(func(t *testing.T, path string) {
		pb := &policyv1alpha1.FileLogSourcePolicy{
			Id:            "fuzz-policy",
			IncludedPaths: []string{path},
		}
		// Validate should not panic on any input
		policy, err := NewPolicyFromProto(pb)
		if err == nil && policy != nil {
			_ = policy.Validate()
			_, _ = policy.Evaluate(context.Background())
		}
	})
}
