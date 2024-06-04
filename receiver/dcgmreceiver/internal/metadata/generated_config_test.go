// Code generated by mdatagen. DO NOT EDIT.

package metadata

import (
	"path/filepath"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/collector/confmap/confmaptest"
)

func TestMetricsBuilderConfig(t *testing.T) {
	tests := []struct {
		name string
		want MetricsBuilderConfig
	}{
		{
			name: "default",
			want: DefaultMetricsBuilderConfig(),
		},
		{
			name: "all_set",
			want: MetricsBuilderConfig{
				Metrics: MetricsConfig{
					DcgmGpuMemoryBytesUsed:            MetricConfig{Enabled: true},
					DcgmGpuProfilingDramUtilization:   MetricConfig{Enabled: true},
					DcgmGpuProfilingNvlinkTrafficRate: MetricConfig{Enabled: true},
					DcgmGpuProfilingPcieTrafficRate:   MetricConfig{Enabled: true},
					DcgmGpuProfilingPipeUtilization:   MetricConfig{Enabled: true},
					DcgmGpuProfilingSmOccupancy:       MetricConfig{Enabled: true},
					DcgmGpuProfilingSmUtilization:     MetricConfig{Enabled: true},
					DcgmGpuUtilization:                MetricConfig{Enabled: true},
				},
			},
		},
		{
			name: "none_set",
			want: MetricsBuilderConfig{
				Metrics: MetricsConfig{
					DcgmGpuMemoryBytesUsed:            MetricConfig{Enabled: false},
					DcgmGpuProfilingDramUtilization:   MetricConfig{Enabled: false},
					DcgmGpuProfilingNvlinkTrafficRate: MetricConfig{Enabled: false},
					DcgmGpuProfilingPcieTrafficRate:   MetricConfig{Enabled: false},
					DcgmGpuProfilingPipeUtilization:   MetricConfig{Enabled: false},
					DcgmGpuProfilingSmOccupancy:       MetricConfig{Enabled: false},
					DcgmGpuProfilingSmUtilization:     MetricConfig{Enabled: false},
					DcgmGpuUtilization:                MetricConfig{Enabled: false},
				},
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := loadMetricsBuilderConfig(t, tt.name)
			if diff := cmp.Diff(tt.want, cfg, cmpopts.IgnoreUnexported(MetricConfig{})); diff != "" {
				t.Errorf("Config mismatch (-expected +actual):\n%s", diff)
			}
		})
	}
}

func loadMetricsBuilderConfig(t *testing.T, name string) MetricsBuilderConfig {
	cm, err := confmaptest.LoadConf(filepath.Join("testdata", "config.yaml"))
	require.NoError(t, err)
	sub, err := cm.Sub(name)
	require.NoError(t, err)
	cfg := DefaultMetricsBuilderConfig()
	require.NoError(t, sub.Unmarshal(&cfg))
	return cfg
}
