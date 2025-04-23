// Copyright 2022 Google LLC
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

package varnishreceiver

import (
	"fmt"
	"net/http"

	"go.opentelemetry.io/collector/pdata/pcommon"

	"github.com/GoogleCloudPlatform/opentelemetry-operations-collector/components/otelopscol/receiver/varnishreceiver/internal/metadata"
)

// FullStats holds stats from a 6.5+ response.
type FullStats struct {
	Version   int    `json:"version"`
	Timestamp string `json:"timestamp"`
	Counters  Stats  `json:"counters"`
}

// Stats holds the metric stats.
type Stats struct {
	MAINBackendConn struct {
		Value int64 `json:"value"`
	} `json:"MAIN.backend_conn"`
	MAINBackendUnhealthy struct {
		Value int64 `json:"value"`
	} `json:"MAIN.backend_unhealthy"`
	MAINBackendBusy struct {
		Value int64 `json:"value"`
	} `json:"MAIN.backend_busy"`
	MAINBackendFail struct {
		Value int64 `json:"value"`
	} `json:"MAIN.backend_fail"`
	MAINBackendReuse struct {
		Value int64 `json:"value"`
	} `json:"MAIN.backend_reuse"`
	MAINBackendRecycle struct {
		Value int64 `json:"value"`
	} `json:"MAIN.backend_recycle"`
	MAINBackendRetry struct {
		Value int64 `json:"value"`
	} `json:"MAIN.backend_retry"`
	MAINCacheHit struct {
		Value int64 `json:"value"`
	} `json:"MAIN.cache_hit"`
	MAINCacheHitpass struct {
		Value int64 `json:"value"`
	} `json:"MAIN.cache_hitpass"`
	MAINCacheMiss struct {
		Value int64 `json:"value"`
	} `json:"MAIN.cache_miss"`
	MAINThreadsCreated struct {
		Value int64 `json:"value"`
	} `json:"MAIN.threads_created"`
	MAINThreadsDestroyed struct {
		Value int64 `json:"value"`
	} `json:"MAIN.threads_destroyed"`
	MAINThreadsFailed struct {
		Value int64 `json:"value"`
	} `json:"MAIN.threads_failed"`
	MAINSessConn struct {
		Value int64 `json:"value"`
	} `json:"MAIN.sess_conn"`
	MAINSessFail struct {
		Value int64 `json:"value"`
	} `json:"MAIN.sess_fail"`
	MAINSessDropped struct {
		Value int64 `json:"value"`
	} `json:"MAIN.sess_dropped"`
	MAINReqDropped struct {
		Value int64 `json:"value"`
	} `json:"MAIN.req_dropped"`
	MAINNObject struct {
		Value int64 `json:"value"`
	} `json:"MAIN.n_object"`
	MAINNExpired struct {
		Value int64 `json:"value"`
	} `json:"MAIN.n_expired"`
	MAINNLruNuked struct {
		Value int64 `json:"value"`
	} `json:"MAIN.n_lru_nuked"`
	MAINNLruMoved struct {
		Value int64 `json:"value"`
	} `json:"MAIN.n_lru_moved"`
	MAINClientReq400 struct {
		Value int64 `json:"value"`
	} `json:"MAIN.client_req_400"`
	MAINClientReq417 struct {
		Value int64 `json:"value"`
	} `json:"MAIN.client_req_417"`
	MAINClientResp500 struct {
		Value int64 `json:"value"`
	} `json:"MAIN.client_resp_500"`
	MAINClientReq struct {
		Value int64 `json:"value"`
	} `json:"MAIN.client_req"`
	MAINBackendReq struct {
		Value int64 `json:"value"`
	} `json:"MAIN.backend_req"`
}

func (v *varnishScraper) recordVarnishBackendConnectionsCountDataPoint(now pcommon.Timestamp, stats *Stats) {
	attributeMappings := map[metadata.AttributeBackendConnectionType]int64{
		metadata.AttributeBackendConnectionTypeSuccess:   stats.MAINBackendConn.Value,
		metadata.AttributeBackendConnectionTypeRecycle:   stats.MAINBackendRecycle.Value,
		metadata.AttributeBackendConnectionTypeReuse:     stats.MAINBackendReuse.Value,
		metadata.AttributeBackendConnectionTypeFail:      stats.MAINBackendFail.Value,
		metadata.AttributeBackendConnectionTypeUnhealthy: stats.MAINBackendUnhealthy.Value,
		metadata.AttributeBackendConnectionTypeBusy:      stats.MAINBackendBusy.Value,
		metadata.AttributeBackendConnectionTypeRetry:     stats.MAINBackendRetry.Value,
	}

	for attributeName, attributeValue := range attributeMappings {
		v.mb.RecordVarnishBackendConnectionCountDataPoint(now, attributeValue, attributeName)
	}
}

func (v *varnishScraper) recordVarnishCacheOperationsCountDataPoint(now pcommon.Timestamp, stats *Stats) {
	attributeMappings := map[metadata.AttributeCacheOperations]int64{
		metadata.AttributeCacheOperationsHit:     stats.MAINCacheHit.Value,
		metadata.AttributeCacheOperationsHitPass: stats.MAINCacheHitpass.Value,
		metadata.AttributeCacheOperationsMiss:    stats.MAINCacheMiss.Value,
	}

	for attributeName, attributeValue := range attributeMappings {
		v.mb.RecordVarnishCacheOperationCountDataPoint(now, attributeValue, attributeName)
	}
}

func (v *varnishScraper) recordVarnishThreadOperationsCountDataPoint(now pcommon.Timestamp, stats *Stats) {
	attributeMappings := map[metadata.AttributeThreadOperations]int64{
		metadata.AttributeThreadOperationsCreated:   stats.MAINThreadsCreated.Value,
		metadata.AttributeThreadOperationsDestroyed: stats.MAINThreadsDestroyed.Value,
		metadata.AttributeThreadOperationsFailed:    stats.MAINThreadsFailed.Value,
	}

	for attributeName, attributeValue := range attributeMappings {
		v.mb.RecordVarnishThreadOperationCountDataPoint(now, attributeValue, attributeName)
	}
}

func (v *varnishScraper) recordVarnishSessionCountDataPoint(now pcommon.Timestamp, stats *Stats) {
	attributeMappings := map[metadata.AttributeSessionType]int64{
		metadata.AttributeSessionTypeAccepted: stats.MAINSessConn.Value,
		metadata.AttributeSessionTypeDropped:  stats.MAINSessDropped.Value,
		metadata.AttributeSessionTypeFailed:   stats.MAINSessFail.Value,
	}

	for attributeName, attributeValue := range attributeMappings {
		v.mb.RecordVarnishSessionCountDataPoint(now, attributeValue, attributeName)
	}
}

func (v *varnishScraper) recordVarnishClientRequestsCountDataPoint(now pcommon.Timestamp, stats *Stats) {
	attributeMappings := map[metadata.AttributeState]int64{
		metadata.AttributeStateReceived: stats.MAINClientReq.Value,
		metadata.AttributeStateDropped:  stats.MAINReqDropped.Value,
	}

	for attributeName, attributeValue := range attributeMappings {
		v.mb.RecordVarnishClientRequestCountDataPoint(now, attributeValue, attributeName)
	}
}

func (v *varnishScraper) recordVarnishClientRequestErrorCountDataPoint(now pcommon.Timestamp, stats *Stats) {
	attributeMappings := map[string]int64{
		fmt.Sprint(http.StatusBadRequest):          stats.MAINClientReq400.Value,
		fmt.Sprint(http.StatusExpectationFailed):   stats.MAINClientReq417.Value,
		fmt.Sprint(http.StatusInternalServerError): stats.MAINClientResp500.Value,
	}

	for attributeName, attributeValue := range attributeMappings {
		v.mb.RecordVarnishClientRequestErrorCountDataPoint(now, attributeValue, attributeName)
	}
}
