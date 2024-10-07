// Copyright The OpenTelemetry Authors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//      http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package mongodbreceiver // import "github.com/GoogleCloudPlatform/opentelemetry-operations-collector/receiver/mongodbreceiver"

import (
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/collector/component"
	"go.opentelemetry.io/collector/config/confignet"
	"go.opentelemetry.io/collector/config/configtls"
	"go.opentelemetry.io/collector/confmap/confmaptest"

	"github.com/GoogleCloudPlatform/opentelemetry-operations-collector/receiver/mongodbreceiver/internal/metadata"
)

func TestValidate(t *testing.T) {
	testCases := []struct {
		endpoints []string
		desc      string
		username  string
		password  string
		expected  error
	}{
		{
			desc:      "no username, no password",
			endpoints: []string{"localhost:27107"},
			username:  "",
			password:  "",
			expected:  nil,
		},
		{
			desc:      "no username, with password",
			endpoints: []string{"localhost:27107"},
			username:  "",
			password:  "pass",
			expected:  errors.New("password provided without user"),
		},
		{
			desc:      "with username, no password",
			endpoints: []string{"localhost:27107"},
			username:  "user",
			password:  "",
			expected:  errors.New("username provided without password"),
		},
		{
			desc:      "with username and password",
			endpoints: []string{"localhost:27107"},
			username:  "user",
			password:  "pass",
			expected:  nil,
		},
		{
			desc:     "no hosts",
			username: "user",
			password: "pass",
			expected: errors.New("no hosts were specified in the config"),
		},
		{
			desc:      "valid hostname",
			endpoints: []string{"localhost"},
			expected:  nil,
		},
		{
			desc:      "empty host",
			username:  "user",
			endpoints: []string{""},
			expected:  errors.New("no endpoint specified for one of the hosts"),
		},
	}
	for _, tc := range testCases {
		t.Run(tc.desc, func(t *testing.T) {
			var hosts []confignet.AddrConfig

			for _, ep := range tc.endpoints {
				hosts = append(hosts, confignet.AddrConfig{
					Endpoint: ep,
				})
			}

			cfg := Config{
				Username: tc.username,
				Password: tc.password,
				Hosts:    hosts,
			}
			err := cfg.Validate()
			if tc.expected == nil {
				require.Nil(t, err)
			} else {
				require.Contains(t, err.Error(), tc.expected.Error())
			}
		})
	}
}

func TestBadTLSConfigs(t *testing.T) {
	testCases := []struct {
		desc        string
		tlsConfig   configtls.ClientConfig
		expectError bool
	}{
		{
			desc: "CA file not found",
			tlsConfig: configtls.ClientConfig{
				Config: configtls.Config{
					CAFile: "not/a/real/file.pem",
				},
				Insecure:           false,
				InsecureSkipVerify: false,
				ServerName:         "",
			},
			expectError: true,
		},
		{
			desc: "no issues",
			tlsConfig: configtls.ClientConfig{
				Config:             configtls.Config{},
				Insecure:           false,
				InsecureSkipVerify: false,
				ServerName:         "",
			},
			expectError: false,
		},
	}
	for _, tc := range testCases {
		t.Run(tc.desc, func(t *testing.T) {
			cfg := Config{
				Username: "otel",
				Password: "pword",
				Hosts: []confignet.AddrConfig{
					{
						Endpoint: "localhost:27017",
					},
				},
				ClientConfig: tc.tlsConfig,
			}
			err := cfg.Validate()
			if tc.expectError {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestOptions(t *testing.T) {
	cfg := &Config{
		Hosts: []confignet.AddrConfig{
			{
				Endpoint: "localhost:27017",
			},
		},
		Username:   "uname",
		Password:   "password",
		Timeout:    2 * time.Minute,
		ReplicaSet: "rs-1",
	}

	clientOptions := cfg.ClientOptions()
	require.Equal(t, clientOptions.Auth.Username, cfg.Username)
	require.Equal(t,
		clientOptions.ConnectTimeout.Milliseconds(),
		(2 * time.Minute).Milliseconds(),
	)
	require.Equal(t, "rs-1", *clientOptions.ReplicaSet)
}

func TestOptionsTLS(t *testing.T) {
	// loading valid ca file
	caFile := filepath.Join("testdata", "certs", "ca.crt")

	cfg := &Config{
		Hosts: []confignet.AddrConfig{
			{
				Endpoint: "localhost:27017",
			},
		},
		ClientConfig: configtls.ClientConfig{
			Insecure: false,
			Config: configtls.Config{
				CAFile: caFile,
			},
		},
	}
	opts := cfg.ClientOptions()
	require.NotNil(t, opts.TLSConfig)
}

func TestLoadConfig(t *testing.T) {
	cm, err := confmaptest.LoadConf(filepath.Join("testdata", "config.yaml"))
	require.NoError(t, err)

	factory := NewFactory()
	cfg := factory.CreateDefaultConfig()

	sub, err := cm.Sub(component.NewIDWithName(metadata.Type, "").String())
	require.NoError(t, err)
	require.NoError(t, sub.Unmarshal(&cfg))

	expected := factory.CreateDefaultConfig().(*Config)
	expected.Hosts = []confignet.AddrConfig{
		{
			Endpoint: "localhost:27017",
		},
	}
	expected.Username = "otel"
	expected.Password = "$MONGO_PASSWORD"
	expected.CollectionInterval = time.Minute

	require.Equal(t, expected, cfg)
}
