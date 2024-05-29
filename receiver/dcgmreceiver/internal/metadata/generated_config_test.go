// Code generated by mdatagen. DO NOT EDIT.

package metadata

import (
	"path/filepath"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/collector/component"
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
					GpuDcgmClockFrequency:             MetricConfig{Enabled: true},
					GpuDcgmClockThrottleDurationTime:  MetricConfig{Enabled: true},
					GpuDcgmCodecDecoderUtilization:    MetricConfig{Enabled: true},
					GpuDcgmCodecEncoderUtilization:    MetricConfig{Enabled: true},
					GpuDcgmEccErrors:                  MetricConfig{Enabled: true},
					GpuDcgmEnergyConsumption:          MetricConfig{Enabled: true},
					GpuDcgmMemoryBandwidthUtilization: MetricConfig{Enabled: true},
					GpuDcgmMemoryBytesUsed:            MetricConfig{Enabled: true},
					GpuDcgmNvlinkTraffic:              MetricConfig{Enabled: true},
					GpuDcgmPcieTraffic:                MetricConfig{Enabled: true},
					GpuDcgmPipeUtilization:            MetricConfig{Enabled: true},
					GpuDcgmSmOccupancy:                MetricConfig{Enabled: true},
					GpuDcgmSmUtilization:              MetricConfig{Enabled: true},
					GpuDcgmTemperature:                MetricConfig{Enabled: true},
					GpuDcgmUtilization:                MetricConfig{Enabled: true},
					GpuDcgmXidErrors:                  MetricConfig{Enabled: true},
				},
				ResourceAttributes: ResourceAttributesConfig{
					GpuModel:  ResourceAttributeConfig{Enabled: true},
					GpuNumber: ResourceAttributeConfig{Enabled: true},
					GpuUUID:   ResourceAttributeConfig{Enabled: true},
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
					GpuDcgmClockFrequency:             MetricConfig{Enabled: false},
					GpuDcgmClockThrottleDurationTime:  MetricConfig{Enabled: false},
					GpuDcgmCodecDecoderUtilization:    MetricConfig{Enabled: false},
					GpuDcgmCodecEncoderUtilization:    MetricConfig{Enabled: false},
					GpuDcgmEccErrors:                  MetricConfig{Enabled: false},
					GpuDcgmEnergyConsumption:          MetricConfig{Enabled: false},
					GpuDcgmMemoryBandwidthUtilization: MetricConfig{Enabled: false},
					GpuDcgmMemoryBytesUsed:            MetricConfig{Enabled: false},
					GpuDcgmNvlinkTraffic:              MetricConfig{Enabled: false},
					GpuDcgmPcieTraffic:                MetricConfig{Enabled: false},
					GpuDcgmPipeUtilization:            MetricConfig{Enabled: false},
					GpuDcgmSmOccupancy:                MetricConfig{Enabled: false},
					GpuDcgmSmUtilization:              MetricConfig{Enabled: false},
					GpuDcgmTemperature:                MetricConfig{Enabled: false},
					GpuDcgmUtilization:                MetricConfig{Enabled: false},
					GpuDcgmXidErrors:                  MetricConfig{Enabled: false},
				},
				ResourceAttributes: ResourceAttributesConfig{
					GpuModel:  ResourceAttributeConfig{Enabled: false},
					GpuNumber: ResourceAttributeConfig{Enabled: false},
					GpuUUID:   ResourceAttributeConfig{Enabled: false},
				},
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := loadMetricsBuilderConfig(t, tt.name)
			if diff := cmp.Diff(tt.want, cfg, cmpopts.IgnoreUnexported(MetricConfig{}, ResourceAttributeConfig{})); diff != "" {
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
	require.NoError(t, component.UnmarshalConfig(sub, &cfg))
	return cfg
}

func TestResourceAttributesConfig(t *testing.T) {
	tests := []struct {
		name string
		want ResourceAttributesConfig
	}{
		{
			name: "default",
			want: DefaultResourceAttributesConfig(),
		},
		{
			name: "all_set",
			want: ResourceAttributesConfig{
				GpuModel:  ResourceAttributeConfig{Enabled: true},
				GpuNumber: ResourceAttributeConfig{Enabled: true},
				GpuUUID:   ResourceAttributeConfig{Enabled: true},
			},
		},
		{
			name: "none_set",
			want: ResourceAttributesConfig{
				GpuModel:  ResourceAttributeConfig{Enabled: false},
				GpuNumber: ResourceAttributeConfig{Enabled: false},
				GpuUUID:   ResourceAttributeConfig{Enabled: false},
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := loadResourceAttributesConfig(t, tt.name)
			if diff := cmp.Diff(tt.want, cfg, cmpopts.IgnoreUnexported(ResourceAttributeConfig{})); diff != "" {
				t.Errorf("Config mismatch (-expected +actual):\n%s", diff)
			}
		})
	}
}

func loadResourceAttributesConfig(t *testing.T, name string) ResourceAttributesConfig {
	cm, err := confmaptest.LoadConf(filepath.Join("testdata", "config.yaml"))
	require.NoError(t, err)
	sub, err := cm.Sub(name)
	require.NoError(t, err)
	sub, err = sub.Sub("resource_attributes")
	require.NoError(t, err)
	cfg := DefaultResourceAttributesConfig()
	require.NoError(t, component.UnmarshalConfig(sub, &cfg))
	return cfg
}
