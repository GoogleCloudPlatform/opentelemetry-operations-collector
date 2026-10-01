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

package opsagentconfprovider

import (
	"context"
	"runtime"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/collector/confmap"
	"go.opentelemetry.io/collector/pdata/pmetric"
)

func TestHostmetricsReceiver(t *testing.T) {
	t.Run("linux_default_interval", func(t *testing.T) {
		conf := confmap.NewFromStringMap(hostmetricsReceiver("", false))
		assert.Equal(t, "60s", conf.Get("collection_interval"))
		assert.Equal(t, []string{"cpu", "state"}, conf.Get("scrapers::cpu::metrics::system.cpu.time::attributes"))
		assert.Equal(t, false, conf.Get("scrapers::cpu::metrics::system.cpu.logical.count::enabled"))
		assert.True(t, conf.IsSet("scrapers::disk"))
		assert.True(t, conf.IsSet("scrapers::filesystem"))
		assert.True(t, conf.IsSet("scrapers::load"))
		assert.True(t, conf.IsSet("scrapers::memory"))
		assert.True(t, conf.IsSet("scrapers::network"))
		assert.True(t, conf.IsSet("scrapers::paging"))
		assert.True(t, conf.IsSet("scrapers::processes"))
		assert.Equal(t, true, conf.Get("scrapers::process::mute_process_name_error"))
		assert.Equal(t, true, conf.Get("scrapers::process::mute_process_exe_error"))
		assert.Equal(t, true, conf.Get("scrapers::process::mute_process_all_errors"))
		assert.False(t, conf.IsSet("scrapers::process::metrics"))
	})

	t.Run("windows_custom_interval", func(t *testing.T) {
		conf := confmap.NewFromStringMap(hostmetricsReceiver("30s", true))
		assert.Equal(t, "30s", conf.Get("collection_interval"))
		assert.Equal(t, true, conf.Get("scrapers::process::metrics::process.handles::enabled"))
	})
}

func TestHostmetricsProcessors(t *testing.T) {
	t.Run("linux", func(t *testing.T) {
		procs := map[string]any{}
		ids := registerProcessors(procs, hostmetricsProcessors("hostmetrics", false))
		assert.Equal(t, []string{
			"agentmetrics/hostmetrics_0",
			"filter/hostmetrics_1",
			"metricstransform/hostmetrics_2",
			"transform/hostmetrics_3",
			"transform/hostmetrics_4",
		}, ids)
		require.Len(t, procs, 5)

		conf := confmap.NewFromStringMap(procs)
		assert.Equal(t, []string{"system.cpu.utilization"}, conf.Get("agentmetrics/hostmetrics_0::blank_label_metrics"))
		assert.Equal(t, []string{
			"system.network.dropped",
			"system.filesystem.inodes.usage",
			"system.paging.faults",
			"system.disk.operation_time",
		}, conf.Get("filter/hostmetrics_1::metrics::exclude::metric_names"))

		transforms, ok := conf.Get("metricstransform/hostmetrics_2::transforms").([]map[string]any)
		require.True(t, ok)
		require.Len(t, transforms, 33)
		assert.Equal(t, "^(.*)$", transforms[len(transforms)-1]["include"])
		assert.Equal(t, "agent.googleapis.com/${1}", transforms[len(transforms)-1]["new_name"])
	})

	t.Run("windows", func(t *testing.T) {
		procs := map[string]any{}
		registerProcessors(procs, hostmetricsProcessors("hostmetrics", true))
		conf := confmap.NewFromStringMap(procs)
		transforms, ok := conf.Get("metricstransform/hostmetrics_2::transforms").([]map[string]any)
		require.True(t, ok)
		require.Len(t, transforms, 34)
		assert.Equal(t, "process.handles", transforms[len(transforms)-2]["include"])
		assert.Equal(t, "processes/windows/handles", transforms[len(transforms)-2]["new_name"])
	})
}

func TestHostmetricsTransformation(t *testing.T) {
	hostIDs := registerProcessors(map[string]any{}, hostmetricsProcessors("hostmetrics", runtime.GOOS == "windows"))
	chain, sink := buildProcessorChain(t, hostIDs)

	md := pmetric.NewMetrics()
	rm := md.ResourceMetrics().AppendEmpty()
	rm.Resource().Attributes().PutStr("service.name", "otelcol")
	rm.Resource().Attributes().PutStr("host.name", "test-vm")

	sm := rm.ScopeMetrics().AppendEmpty()
	sm.Scope().SetName("otelcol/hostmetricsreceiver")
	sm.Scope().SetVersion("0.162.0")

	// 1. system.cpu.time (Cumulative Sum, Double, cpu="cpu0")
	mCPU := sm.Metrics().AppendEmpty()
	mCPU.SetName("system.cpu.time")
	cpuSum := mCPU.SetEmptySum()
	cpuSum.SetIsMonotonic(true)
	cpuSum.SetAggregationTemporality(pmetric.AggregationTemporalityCumulative)
	dpCPU := cpuSum.DataPoints().AppendEmpty()
	dpCPU.SetDoubleValue(12.0)
	dpCPU.Attributes().PutStr("cpu", "cpu0")
	dpCPU.Attributes().PutStr("state", "user")

	// 2. system.disk.io (split by agentmetrics into read_io and write_io)
	mDiskIO := sm.Metrics().AppendEmpty()
	mDiskIO.SetName("system.disk.io")
	diskSum := mDiskIO.SetEmptySum()
	diskSum.SetIsMonotonic(true)
	diskSum.SetAggregationTemporality(pmetric.AggregationTemporalityCumulative)
	dpRead := diskSum.DataPoints().AppendEmpty()
	dpRead.SetIntValue(1024)
	dpRead.Attributes().PutStr("device", "sda")
	dpRead.Attributes().PutStr("direction", "read")
	dpWrite := diskSum.DataPoints().AppendEmpty()
	dpWrite.SetIntValue(2048)
	dpWrite.Attributes().PutStr("device", "sda")
	dpWrite.Attributes().PutStr("direction", "write")

	// 3. system.memory.usage (non-monotonic Sum -> converted to Gauge + utilization calculated by agentmetrics;
	// slab_reclaimable + slab_unreclaimable aggregated into slab by metricstransform)
	mMem := sm.Metrics().AppendEmpty()
	mMem.SetName("system.memory.usage")
	memSum := mMem.SetEmptySum()
	memSum.SetIsMonotonic(false)
	memSum.SetAggregationTemporality(pmetric.AggregationTemporalityCumulative)
	dpMemUsed := memSum.DataPoints().AppendEmpty()
	dpMemUsed.SetIntValue(500)
	dpMemUsed.Attributes().PutStr("state", "used")
	dpSlabRec := memSum.DataPoints().AppendEmpty()
	dpSlabRec.SetIntValue(200)
	dpSlabRec.Attributes().PutStr("state", "slab_reclaimable")
	dpSlabUnrec := memSum.DataPoints().AppendEmpty()
	dpSlabUnrec.SetIntValue(300)
	dpSlabUnrec.Attributes().PutStr("state", "slab_unreclaimable")

	// 4. system.network.io (interface -> device, receive -> rx)
	mNet := sm.Metrics().AppendEmpty()
	mNet.SetName("system.network.io")
	netSum := mNet.SetEmptySum()
	netSum.SetIsMonotonic(true)
	netSum.SetAggregationTemporality(pmetric.AggregationTemporalityCumulative)
	dpNet := netSum.DataPoints().AppendEmpty()
	dpNet.SetIntValue(4096)
	dpNet.Attributes().PutStr("interface", "eth0")
	dpNet.Attributes().PutStr("direction", "receive")

	// 5. process.cpu.time (scaled by 1e6, wait deleted, system -> syst, process=all added)
	mProcCPU := sm.Metrics().AppendEmpty()
	mProcCPU.SetName("process.cpu.time")
	procSum := mProcCPU.SetEmptySum()
	procSum.SetIsMonotonic(true)
	procSum.SetAggregationTemporality(pmetric.AggregationTemporalityCumulative)
	dpProcSys := procSum.DataPoints().AppendEmpty()
	dpProcSys.SetDoubleValue(1.5)
	dpProcSys.Attributes().PutStr("state", "system")
	dpProcWait := procSum.DataPoints().AppendEmpty()
	dpProcWait.SetDoubleValue(0.5)
	dpProcWait.Attributes().PutStr("state", "wait")

	// 6. Excluded metric (system.network.dropped -> filtered out)
	mDropped := sm.Metrics().AppendEmpty()
	mDropped.SetName("system.network.dropped")
	mDropped.SetEmptySum().DataPoints().AppendEmpty().SetIntValue(99)

	require.NoError(t, chain.ConsumeMetrics(context.Background(), md))

	all := sink.AllMetrics()
	require.Len(t, all, 1)
	outRM := all[0].ResourceMetrics().At(0)

	// Verify resource and instrumentation scope cleanup.
	_, hasServiceName := outRM.Resource().Attributes().Get("service.name")
	assert.False(t, hasServiceName)
	hostVal, hasHost := outRM.Resource().Attributes().Get("host.name")
	require.True(t, hasHost)
	assert.Equal(t, "test-vm", hostVal.Str())

	outSM := outRM.ScopeMetrics().At(0)
	assert.Empty(t, outSM.Scope().Name())
	assert.Empty(t, outSM.Scope().Version())

	gotMetrics := make(map[string]pmetric.Metric)
	for i := 0; i < outSM.Metrics().Len(); i++ {
		m := outSM.Metrics().At(i)
		gotMetrics[m.Name()] = m
	}

	assert.NotContains(t, gotMetrics, "system.network.dropped")
	assert.NotContains(t, gotMetrics, "agent.googleapis.com/system.network.dropped")

	// 1. cpu/usage_time: cpu0 -> "0", state -> cpu_state, double -> int64
	require.Contains(t, gotMetrics, "agent.googleapis.com/cpu/usage_time")
	cpuDP := gotMetrics["agent.googleapis.com/cpu/usage_time"].Sum().DataPoints().At(0)
	assert.Equal(t, int64(12), cpuDP.IntValue())
	cpuNum, ok := cpuDP.Attributes().Get("cpu_number")
	require.True(t, ok)
	assert.Equal(t, "0", cpuNum.Str())
	cpuState, ok := cpuDP.Attributes().Get("cpu_state")
	require.True(t, ok)
	assert.Equal(t, "user", cpuState.Str())

	// 2. disk/read_bytes_count and disk/write_bytes_count
	require.Contains(t, gotMetrics, "agent.googleapis.com/disk/read_bytes_count")
	require.Contains(t, gotMetrics, "agent.googleapis.com/disk/write_bytes_count")
	assert.Equal(t, int64(1024), gotMetrics["agent.googleapis.com/disk/read_bytes_count"].Sum().DataPoints().At(0).IntValue())
	assert.Equal(t, int64(2048), gotMetrics["agent.googleapis.com/disk/write_bytes_count"].Sum().DataPoints().At(0).IntValue())

	// 3. memory/bytes_used (Gauge, toggled int64 -> double) and memory/percent_used (computed by agentmetrics + slab aggregated)
	require.Contains(t, gotMetrics, "agent.googleapis.com/memory/bytes_used")
	require.Contains(t, gotMetrics, "agent.googleapis.com/memory/percent_used")
	memDPs := gotMetrics["agent.googleapis.com/memory/bytes_used"].Gauge().DataPoints()
	require.Equal(t, 2, memDPs.Len())
	memByState := make(map[string]float64)
	for i := 0; i < memDPs.Len(); i++ {
		st, _ := memDPs.At(i).Attributes().Get("state")
		memByState[st.Str()] = memDPs.At(i).DoubleValue()
	}
	assert.Equal(t, map[string]float64{"used": 500, "slab": 500}, memByState)

	// 4. interface/traffic (interface -> device, receive -> rx)
	require.Contains(t, gotMetrics, "agent.googleapis.com/interface/traffic")
	netDP := gotMetrics["agent.googleapis.com/interface/traffic"].Sum().DataPoints().At(0)
	devVal, ok := netDP.Attributes().Get("device")
	require.True(t, ok)
	assert.Equal(t, "eth0", devVal.Str())
	dirVal, ok := netDP.Attributes().Get("direction")
	require.True(t, ok)
	assert.Equal(t, "rx", dirVal.Str())

	// 5. processes/cpu_time (1.5 * 1e6 = 1500000, wait deleted, system -> syst, process=all)
	require.Contains(t, gotMetrics, "agent.googleapis.com/processes/cpu_time")
	procDPs := gotMetrics["agent.googleapis.com/processes/cpu_time"].Sum().DataPoints()
	require.Equal(t, 1, procDPs.Len())
	assert.Equal(t, int64(1500000), procDPs.At(0).IntValue())
	procLabel, ok := procDPs.At(0).Attributes().Get("process")
	require.True(t, ok)
	assert.Equal(t, "all", procLabel.Str())
	userOrSyst, ok := procDPs.At(0).Attributes().Get("user_or_syst")
	require.True(t, ok)
	assert.Equal(t, "syst", userOrSyst.Str())
}
