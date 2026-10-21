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
		{"/var/log"},
		{"/opt/google/logs/*.log"},
		{"/srv/web/logs/**/*.log"},
		{"/var/log/syslog", "/opt/app/logs/server.log"},
		{"/var/log/app.log", "/var/log/App.log"},
	}

	for _, paths := range validPaths {
		t.Run(paths[0], func(t *testing.T) {
			pb := &policyv1alpha1.FileLogSourcePolicy{
				Id:      "valid-policy",
				Include: paths,
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
		{`%SystemRoot%\inetpub\logs\*.log`},
	}

	for _, paths := range validPaths {
		t.Run(paths[0], func(t *testing.T) {
			pb := &policyv1alpha1.FileLogSourcePolicy{
				Id:      "windows-policy",
				Include: paths,
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
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			pb := &policyv1alpha1.FileLogSourcePolicy{
				Id:      tc.id,
				Include: tc.include,
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
		Id:      "test-log-source",
		Include: []string{"/var/log/test.log"},
	}
	p, err := NewPolicyFromProto(pb)
	require.NoError(t, err)

	assert.Equal(t, "test-log-source", p.PolicyName())
	assert.Equal(t, PolicyType, p.PolicyType())
	assert.Equal(t, googlepolicy.PolicyClassSource, p.PolicyClass())
	assert.Equal(t, []string{"/var/log/test.log"}, p.Include())

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
	assert.Nil(t, empty.Include())
	assert.ErrorIs(t, empty.Validate(), ErrNilProto)
	_, err = NewPolicyFromProto(nil)
	assert.ErrorIs(t, err, ErrNilProto)
}

func TestPolicy_Evaluate(t *testing.T) {
	pb := &policyv1alpha1.FileLogSourcePolicy{
		Id:      "projects/123/locations/global/policySets/ps1/policies/app-logs",
		Include: []string{"/var/log/app/*.log"},
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

func TestPolicy_ReceiverIDs(t *testing.T) {
	pb := &policyv1alpha1.FileLogSourcePolicy{
		Id:      "nginx-logs",
		Include: []string{"/var/log/nginx/*.log"},
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
		Id:      "single-policy",
		Include: []string{"/var/log/single/*.log"},
	})
	require.NoError(t, err)

	merged, err := Merge(p)
	require.NoError(t, err)
	assert.Equal(t, p, merged)
	assert.Equal(t, "single-policy", merged.PolicyName())
	assert.Equal(t, []string{"/var/log/single/*.log"}, merged.Include())
}

func TestMerge_MultiplePolicies_DeduplicatedAndSorted(t *testing.T) {
	p1, err := NewPolicyFromProto(&policyv1alpha1.FileLogSourcePolicy{
		Id:      "policy-1",
		Include: []string{"/var/log/syslog", "/var/log/nginx/*.log"},
	})
	require.NoError(t, err)

	p2, err := NewPolicyFromProto(&policyv1alpha1.FileLogSourcePolicy{
		Id:      "policy-2",
		Include: []string{"/var/log/nginx/*.log", "/var/log/auth.log"},
	})
	require.NoError(t, err)

	p3, err := NewPolicyFromProto(&policyv1alpha1.FileLogSourcePolicy{
		Id:      "policy-3",
		Include: []string{"/opt/app/logs/*.log", "/var/log/syslog"},
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
	}, merged.Include())

	// Verify merged policy evaluation uses DefaultReceiverID and DefaultPipelineID.
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
		Id:      "win-1",
		Include: []string{`C:\ProgramData\App\logs\app.log`},
	})
	require.NoError(t, err)

	p2, err := NewPolicyFromProto(&policyv1alpha1.FileLogSourcePolicy{
		Id:      "win-2",
		Include: []string{`c:/programdata/app/logs/app.log`},
	})
	require.NoError(t, err)

	merged, err := Merge(p1, p2)
	require.NoError(t, err)
	require.NotNil(t, merged)
	assert.Len(t, merged.Include(), 1)
	assert.Equal(t, `C:\ProgramData\App\logs\app.log`, merged.Include()[0])
}

func TestMerge_FailsFastOnInvalidPolicy(t *testing.T) {
	valid, err := NewPolicyFromProto(&policyv1alpha1.FileLogSourcePolicy{
		Id:      "valid",
		Include: []string{"/var/log/valid.log"},
	})
	require.NoError(t, err)

	invalid := &Policy{
		proto: &policyv1alpha1.FileLogSourcePolicy{
			Id:      "invalid",
			Include: []string{"/etc/shadow"},
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
		Id:      "p1",
		Include: []string{"/var/log/a.log"},
	})
	require.NoError(t, err)

	p2, err := NewPolicyFromProto(&policyv1alpha1.FileLogSourcePolicy{
		Id:      "p2",
		Include: []string{"/var/log/b.log"},
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

	// Consolidate multiple
	merged, err = p1.Consolidate([]googlepolicy.SourcePolicy{p1, p2})
	require.NoError(t, err)
	require.NotNil(t, merged)
	assert.Equal(t, "filelog_consolidated", merged.PolicyName())
	assert.Equal(t, []string{"/var/log/a.log", "/var/log/b.log"}, merged.(interface{ Include() []string }).Include())
}

func TestDriver_WireProtoAndMapLoading(t *testing.T) {
	pb := &policyv1alpha1.FileLogSourcePolicy{
		Id:      "wire-log-source",
		Include: []string{"/var/log/**/*.log"},
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
			"type":    PolicyType,
			"id":      "authored-log-source",
			"include": []any{"/opt/services/logs/*.log"},
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
	assert.Equal(t, []string{"/opt/services/logs/*.log"}, srcPolicy.Include())
}

func TestPolicyValidation_TestAllowedRoot(t *testing.T) {
	// Verify that testing.Testing() returns true in this test execution context.
	require.True(t, testing.Testing(), "testing.Testing() must return true under go test")

	tempDir := t.TempDir()

	// 1. Without FILELOG_TEST_ALLOWED_ROOT, paths in tempDir are rejected.
	t.Setenv("FILELOG_TEST_ALLOWED_ROOT", "")
	pbUnset := &policyv1alpha1.FileLogSourcePolicy{
		Id:      "temp-dir-unset-policy",
		Include: []string{tempDir + "/*.log"},
	}
	_, err := NewPolicyFromProto(pbUnset)
	require.ErrorIs(t, err, ErrDisallowedLogRoot)

	// 2. With FILELOG_TEST_ALLOWED_ROOT set, paths within tempDir are accepted.
	t.Setenv("FILELOG_TEST_ALLOWED_ROOT", tempDir)
	pbSet := &policyv1alpha1.FileLogSourcePolicy{
		Id:      "temp-dir-policy",
		Include: []string{tempDir + "/*.log"},
	}
	polSet, err := NewPolicyFromProto(pbSet)
	require.NoError(t, err)
	assert.NoError(t, polSet.Validate())

	// 3. Sensitive directories are strictly rejected even when FILELOG_TEST_ALLOWED_ROOT is set.
	pbSensitive := &policyv1alpha1.FileLogSourcePolicy{
		Id:      "sensitive-policy",
		Include: []string{"/etc/shadow"},
	}
	_, err = NewPolicyFromProto(pbSensitive)
	require.ErrorIs(t, err, ErrSensitiveDirectory)

	// 4. Path traversal escaping the root is strictly rejected.
	pbTraversal := &policyv1alpha1.FileLogSourcePolicy{
		Id:      "traversal-policy",
		Include: []string{tempDir + "/../etc/shadow"},
	}
	_, err = NewPolicyFromProto(pbTraversal)
	require.ErrorIs(t, err, ErrPathTraversal)

	// 5. Windows path matching under FILELOG_TEST_ALLOWED_ROOT (case-insensitive and separator-normalized).
	t.Setenv("FILELOG_TEST_ALLOWED_ROOT", "C:\\TestRoot\\logs")
	pbWin := &policyv1alpha1.FileLogSourcePolicy{
		Id:      "win-test-policy",
		Include: []string{"c:/testroot/logs/app.log", "C:\\TestRoot\\logs\\sub\\*.log"},
	}
	polWin, err := NewPolicyFromProto(pbWin)
	require.NoError(t, err)
	assert.NoError(t, polWin.Validate())
}
