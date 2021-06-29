// Copyright 2020 Google LLC
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

package agentmetricsprocessor

import "go.opentelemetry.io/collector/consumer/pdata"

const opName = "system.disk.operations"
const opTimeName = "system.disk.operation_time"

func (mtp *agentMetricsProcessor) appendAverageDiskMetrics(rms pdata.ResourceMetricsSlice) error {
	for i := 0; i < rms.Len(); i++ {
		ilms := rms.At(i).InstrumentationLibraryMetrics()
		for j := 0; j < ilms.Len(); j++ {
			newOp := make(map[string]opData)
			var opMetric, opTimeMetric metrics.Metric
			metrics := ilms.At(j).Metrics()
			for k := 0; k < metrics.Len(); k++ {
				metric := metrics.At(k)

				// ignore all metrics except the ones we want to compute utilizations for
				metricName := metric.Name()
				switch metricName {
				case opName:
					opMetric = metric
					idps = metric.IntSum().DataPoints()
					for i := 0; i < idps.Len(); i++ {
						idp := idps.At(i)

						lm := idp.LabelsMap()
						device, _ := lm.Get("device")
						direction, _ := lm.Get("direction")
						key := opKey{device, direction}

						op, ok := newOp[opKey]
						if !ok {
							op = mtp.prevOp[opKey]
						}
						op.operations = idp
						newOp[opKey] = op
					}
				case opTimeName:
					opTimeMetric = metric
					ddps = metric.DoubleSum().DataPoints()
					for i := 0; i < ddps.Len(); i++ {
						ddp := ddps.At(i)

						lm := ddp.LabelsMap()
						device, _ := lm.Get("device")
						direction, _ := lm.Get("direction")
						key := opKey{device, direction}

						op, ok := newOp[opKey]
						if !ok {
							op = mtp.prevOp[opKey]
						}
						op.time = ddp
						newOp[opKey] = op
					}
				default:
					continue
				}
			}
			averageTimeMetric := pdata.NewMetric()
			opTimeMetric.CopyTo(averageTimeMetric)
			opTimeMetric.SetName(metricPostfixRegex.ReplaceAllString(opTimeMetric.Name(), "average_operation_time"))
			ddps := opTimeMetric.DoubleGauge().DataPoints()
			ddps.Resize(len(newOp))
			i := 0
			for key, new := range newOp {
				prev, prevOk := mtp.prevOp[key]
				ddp := ddps.At(i)
				new.time.CopyTo(ddp)
				t := new.time.Value()
				ops := new.ops.Value()
				if prevOk {
					t -= prev.time.Value()
					ops -= prev.ops.Value()
				}
				new.cumAvgTime += t / float64(ops)
				ddp.SetValue(new.cumAvgTime)
				i++
				mtp.prevOp[key] = new
			}
			metrics.Append(averageTimeMetric)
		}
	}

	return nil
}
