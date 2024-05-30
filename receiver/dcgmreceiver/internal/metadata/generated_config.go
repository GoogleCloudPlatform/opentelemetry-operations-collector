// Code generated by mdatagen. DO NOT EDIT.

package metadata

import (
	"go.opentelemetry.io/collector/confmap"
	"go.opentelemetry.io/collector/filter"
)

// MetricConfig provides common config for a particular metric.
type MetricConfig struct {
	Enabled bool `mapstructure:"enabled"`

	enabledSetByUser bool
}

func (ms *MetricConfig) Unmarshal(parser *confmap.Conf) error {
	if parser == nil {
		return nil
	}
	err := parser.Unmarshal(ms)
	if err != nil {
		return err
	}
	ms.enabledSetByUser = parser.IsSet("enabled")
	return nil
}

// MetricsConfig provides config for dcgm metrics.
type MetricsConfig struct {
	GpuDcgmClockFrequency             MetricConfig `mapstructure:"gpu.dcgm.clock.frequency"`
	GpuDcgmClockThrottleDurationTime  MetricConfig `mapstructure:"gpu.dcgm.clock.throttle_duration.time"`
	GpuDcgmCodecDecoderUtilization    MetricConfig `mapstructure:"gpu.dcgm.codec.decoder.utilization"`
	GpuDcgmCodecEncoderUtilization    MetricConfig `mapstructure:"gpu.dcgm.codec.encoder.utilization"`
	GpuDcgmEccErrors                  MetricConfig `mapstructure:"gpu.dcgm.ecc_errors"`
	GpuDcgmEnergyConsumption          MetricConfig `mapstructure:"gpu.dcgm.energy_consumption"`
	GpuDcgmMemoryBandwidthUtilization MetricConfig `mapstructure:"gpu.dcgm.memory.bandwidth_utilization"`
	GpuDcgmMemoryBytesUsed            MetricConfig `mapstructure:"gpu.dcgm.memory.bytes_used"`
	GpuDcgmNvlinkTraffic              MetricConfig `mapstructure:"gpu.dcgm.nvlink.traffic"`
	GpuDcgmPcieTraffic                MetricConfig `mapstructure:"gpu.dcgm.pcie.traffic"`
	GpuDcgmPipeUtilization            MetricConfig `mapstructure:"gpu.dcgm.pipe.utilization"`
	GpuDcgmSmOccupancy                MetricConfig `mapstructure:"gpu.dcgm.sm.occupancy"`
	GpuDcgmSmUtilization              MetricConfig `mapstructure:"gpu.dcgm.sm.utilization"`
	GpuDcgmTemperature                MetricConfig `mapstructure:"gpu.dcgm.temperature"`
	GpuDcgmUtilization                MetricConfig `mapstructure:"gpu.dcgm.utilization"`
	GpuDcgmXidErrors                  MetricConfig `mapstructure:"gpu.dcgm.xid_errors"`
}

func DefaultMetricsConfig() MetricsConfig {
	return MetricsConfig{
		GpuDcgmClockFrequency: MetricConfig{
			Enabled: true,
		},
		GpuDcgmClockThrottleDurationTime: MetricConfig{
			Enabled: true,
		},
		GpuDcgmCodecDecoderUtilization: MetricConfig{
			Enabled: true,
		},
		GpuDcgmCodecEncoderUtilization: MetricConfig{
			Enabled: true,
		},
		GpuDcgmEccErrors: MetricConfig{
			Enabled: true,
		},
		GpuDcgmEnergyConsumption: MetricConfig{
			Enabled: true,
		},
		GpuDcgmMemoryBandwidthUtilization: MetricConfig{
			Enabled: true,
		},
		GpuDcgmMemoryBytesUsed: MetricConfig{
			Enabled: true,
		},
		GpuDcgmNvlinkTraffic: MetricConfig{
			Enabled: true,
		},
		GpuDcgmPcieTraffic: MetricConfig{
			Enabled: true,
		},
		GpuDcgmPipeUtilization: MetricConfig{
			Enabled: true,
		},
		GpuDcgmSmOccupancy: MetricConfig{
			Enabled: false,
		},
		GpuDcgmSmUtilization: MetricConfig{
			Enabled: true,
		},
		GpuDcgmTemperature: MetricConfig{
			Enabled: true,
		},
		GpuDcgmUtilization: MetricConfig{
			Enabled: true,
		},
		GpuDcgmXidErrors: MetricConfig{
			Enabled: true,
		},
	}
}

// ResourceAttributeConfig provides common config for a particular resource attribute.
type ResourceAttributeConfig struct {
	Enabled bool `mapstructure:"enabled"`
	// Experimental: MetricsInclude defines a list of filters for attribute values.
	// If the list is not empty, only metrics with matching resource attribute values will be emitted.
	MetricsInclude []filter.Config `mapstructure:"metrics_include"`
	// Experimental: MetricsExclude defines a list of filters for attribute values.
	// If the list is not empty, metrics with matching resource attribute values will not be emitted.
	// MetricsInclude has higher priority than MetricsExclude.
	MetricsExclude []filter.Config `mapstructure:"metrics_exclude"`

	enabledSetByUser bool
}

func (rac *ResourceAttributeConfig) Unmarshal(parser *confmap.Conf) error {
	if parser == nil {
		return nil
	}
	err := parser.Unmarshal(rac)
	if err != nil {
		return err
	}
	rac.enabledSetByUser = parser.IsSet("enabled")
	return nil
}

// ResourceAttributesConfig provides config for dcgm resource attributes.
type ResourceAttributesConfig struct {
	GpuModel  ResourceAttributeConfig `mapstructure:"gpu.model"`
	GpuNumber ResourceAttributeConfig `mapstructure:"gpu.number"`
	GpuUUID   ResourceAttributeConfig `mapstructure:"gpu.uuid"`
}

func DefaultResourceAttributesConfig() ResourceAttributesConfig {
	return ResourceAttributesConfig{
		GpuModel: ResourceAttributeConfig{
			Enabled: true,
		},
		GpuNumber: ResourceAttributeConfig{
			Enabled: true,
		},
		GpuUUID: ResourceAttributeConfig{
			Enabled: true,
		},
	}
}

// MetricsBuilderConfig is a configuration for dcgm metrics builder.
type MetricsBuilderConfig struct {
	Metrics            MetricsConfig            `mapstructure:"metrics"`
	ResourceAttributes ResourceAttributesConfig `mapstructure:"resource_attributes"`
}

func DefaultMetricsBuilderConfig() MetricsBuilderConfig {
	return MetricsBuilderConfig{
		Metrics:            DefaultMetricsConfig(),
		ResourceAttributes: DefaultResourceAttributesConfig(),
	}
}
