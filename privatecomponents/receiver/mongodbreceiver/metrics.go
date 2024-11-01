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
	"fmt"
	"reflect"

	"github.com/hashicorp/go-version"
	"go.mongodb.org/mongo-driver/bson"
	"go.opentelemetry.io/collector/pdata/pcommon"
	"go.opentelemetry.io/collector/receiver/scrapererror"

	"github.com/GoogleCloudPlatform/opentelemetry-operations-collector/privatecomponents/receiver/mongodbreceiver/internal/metadata"
)

var errKeyNotFound = errors.New("could not find key for metric")

var operationsMap = map[string]metadata.AttributeOperation{
	"insert":   metadata.AttributeOperationInsert,
	"queries":  metadata.AttributeOperationQuery,
	"update":   metadata.AttributeOperationUpdate,
	"remove":   metadata.AttributeOperationDelete,
	"getmore":  metadata.AttributeOperationGetmore,
	"commands": metadata.AttributeOperationCommand,
}

var documentMap = map[string]metadata.AttributeOperation{
	"inserted": metadata.AttributeOperationInsert,
	"updated":  metadata.AttributeOperationUpdate,
	"deleted":  metadata.AttributeOperationDelete,
}

var lockTypeMap = map[string]metadata.AttributeLockType{
	"ParallelBatchWriterMode":    metadata.AttributeLockTypeParallelBatchWriteMode,
	"ReplicationStateTransition": metadata.AttributeLockTypeReplicationStateTransition,
	"Global":                     metadata.AttributeLockTypeGlobal,
	"Database":                   metadata.AttributeLockTypeDatabase,
	"Collection":                 metadata.AttributeLockTypeCollection,
	"Mutex":                      metadata.AttributeLockTypeMutex,
	"Metadata":                   metadata.AttributeLockTypeMetadata,
	"oplog":                      metadata.AttributeLockTypeOplog,
}

var lockModeMap = map[string]metadata.AttributeLockMode{
	"R": metadata.AttributeLockModeShared,
	"W": metadata.AttributeLockModeExclusive,
	"r": metadata.AttributeLockModeIntentShared,
	"w": metadata.AttributeLockModeIntentExclusive,
}

const (
	collectMetricError          = "failed to collect metric %s: %w"
	collectMetricWithAttributes = "failed to collect metric %s with attribute(s) %s: %w"
)

// DBStats
func (s *mongodbScraper) recordCollections(now pcommon.Timestamp, doc bson.M, dbName string, errs *scrapererror.ScrapeErrors) {
	metricPath := []string{"collections"}
	metricName := "mongodb.collection.count"
	val, err := collectMetric(doc, metricPath)
	if err != nil {
		errs.AddPartial(1, fmt.Errorf(collectMetricWithAttributes, metricName, dbName, err))
		return
	}
	s.mb.RecordMongodbCollectionCountDataPoint(now, val, dbName)
}

func (s *mongodbScraper) recordDataSize(now pcommon.Timestamp, doc bson.M, dbName string, errs *scrapererror.ScrapeErrors) {
	metricPath := []string{"dataSize"}
	metricName := "mongodb.data.size"
	val, err := collectMetric(doc, metricPath)
	if err != nil {
		errs.AddPartial(1, fmt.Errorf(collectMetricWithAttributes, metricName, dbName, err))
		return
	}
	s.mb.RecordMongodbDataSizeDataPoint(now, val, dbName)
}

func (s *mongodbScraper) recordStorageSize(now pcommon.Timestamp, doc bson.M, dbName string, errs *scrapererror.ScrapeErrors) {
	metricPath := []string{"storageSize"}
	metricName := "mongodb.storage.size"
	val, err := collectMetric(doc, metricPath)
	if err != nil {
		errs.AddPartial(1, fmt.Errorf(collectMetricWithAttributes, metricName, dbName, err))
		return
	}
	s.mb.RecordMongodbStorageSizeDataPoint(now, val, dbName)
}

func (s *mongodbScraper) recordObjectCount(now pcommon.Timestamp, doc bson.M, dbName string, errs *scrapererror.ScrapeErrors) {
	metricPath := []string{"objects"}
	metricName := "mongodb.object.count"
	val, err := collectMetric(doc, metricPath)
	if err != nil {
		errs.AddPartial(1, fmt.Errorf(collectMetricWithAttributes, metricName, dbName, err))
		return
	}
	s.mb.RecordMongodbObjectCountDataPoint(now, val, dbName)
}

func (s *mongodbScraper) recordIndexCount(now pcommon.Timestamp, doc bson.M, dbName string, errs *scrapererror.ScrapeErrors) {
	metricPath := []string{"indexes"}
	metricName := "mongodb.index.count"
	val, err := collectMetric(doc, metricPath)
	if err != nil {
		errs.AddPartial(1, fmt.Errorf(collectMetricWithAttributes, metricName, dbName, err))
		return
	}
	s.mb.RecordMongodbIndexCountDataPoint(now, val, dbName)
}

func (s *mongodbScraper) recordIndexSize(now pcommon.Timestamp, doc bson.M, dbName string, errs *scrapererror.ScrapeErrors) {
	metricPath := []string{"indexSize"}
	metricName := "mongodb.index.size"
	val, err := collectMetric(doc, metricPath)
	if err != nil {
		errs.AddPartial(1, fmt.Errorf(collectMetricWithAttributes, metricName, dbName, err))
		return
	}
	s.mb.RecordMongodbIndexSizeDataPoint(now, val, dbName)
}

func (s *mongodbScraper) recordExtentCount(now pcommon.Timestamp, doc bson.M, dbName string, errs *scrapererror.ScrapeErrors) {
	// Mongo version 4.4+ no longer returns numExtents since it is part of the obsolete MMAPv1
	// https://www.mongodb.com/docs/manual/release-notes/4.4-compatibility/#mmapv1-cleanup
	mongo44, _ := version.NewVersion("4.4")
	if s.mongoVersion.LessThan(mongo44) {
		metricPath := []string{"numExtents"}
		metricName := "mongodb.extent.count"
		val, err := collectMetric(doc, metricPath)
		if err != nil {
			errs.AddPartial(1, fmt.Errorf(collectMetricWithAttributes, metricName, dbName, err))
			return
		}
		s.mb.RecordMongodbExtentCountDataPoint(now, val, dbName)
	}
}

// ServerStatus
func (s *mongodbScraper) recordConnections(now pcommon.Timestamp, doc bson.M, dbName string, errs *scrapererror.ScrapeErrors) {
	mongo40, _ := version.NewVersion("4.0")
	for ctVal, ct := range metadata.MapAttributeConnectionType {
		// Mongo version 4.0 added active
		// reference: https://www.mongodb.com/docs/v4.0/reference/command/serverStatus/#serverstatus.connections.active
		if s.mongoVersion.LessThan(mongo40) && ctVal == "active" {
			continue
		}
		metricPath := []string{"connections", ctVal}
		metricName := "mongodb.connection.count"
		metricAttributes := fmt.Sprintf("%s, %s", ctVal, dbName)
		val, err := collectMetric(doc, metricPath)
		if err != nil {
			errs.AddPartial(1, fmt.Errorf(collectMetricWithAttributes, metricName, metricAttributes, err))
			continue
		}
		s.mb.RecordMongodbConnectionCountDataPoint(now, val, dbName, ct)
	}
}

func (s *mongodbScraper) recordMemoryUsage(now pcommon.Timestamp, doc bson.M, dbName string, errs *scrapererror.ScrapeErrors) {
	for mtVal, mt := range metadata.MapAttributeMemoryType {
		metricPath := []string{"mem", mtVal}
		metricName := "mongodb.memory.usage"
		metricAttributes := fmt.Sprintf("%s, %s", mtVal, dbName)
		val, err := collectMetric(doc, metricPath)
		if err != nil {
			errs.AddPartial(1, fmt.Errorf(collectMetricWithAttributes, metricName, metricAttributes, err))
			continue
		}
		// convert from mebibytes to bytes
		memUsageBytes := val * int64(1048576)
		s.mb.RecordMongodbMemoryUsageDataPoint(now, memUsageBytes, dbName, mt)
	}
}

func (s *mongodbScraper) recordDocumentOperations(now pcommon.Timestamp, doc bson.M, dbName string, errs *scrapererror.ScrapeErrors) {
	for operationKey, metadataKey := range documentMap {
		metricPath := []string{"metrics", "document", operationKey}
		metricName := "mongodb.document.operation.count"
		metricAttributes := fmt.Sprintf("%s, %s", operationKey, dbName)
		val, err := collectMetric(doc, metricPath)
		if err != nil {
			errs.AddPartial(1, fmt.Errorf(collectMetricWithAttributes, metricName, metricAttributes, err))
			continue
		}
		s.mb.RecordMongodbDocumentOperationCountDataPoint(now, val, dbName, metadataKey)
	}
}

func (s *mongodbScraper) recordSessionCount(now pcommon.Timestamp, doc bson.M, errs *scrapererror.ScrapeErrors) {
	// Collect session count for version 3.0+
	// https://www.mongodb.com/docs/v3.0/reference/command/serverStatus/#serverStatus.wiredTiger.session
	mongo30, _ := version.NewVersion("3.0")
	if s.mongoVersion.LessThan(mongo30) {
		return
	}

	storageEngine, err := dig(doc, []string{"storageEngine", "name"})
	if err != nil {
		errs.AddPartial(1, errors.New("failed to find storage engine for session count"))
		return
	}
	if storageEngine != "wiredTiger" {
		// mongodb is using a different storage engine and this metric can not be collected
		return
	}

	metricPath := []string{"wiredTiger", "session", "open session count"}
	metricName := "mongodb.session.count"
	val, err := collectMetric(doc, metricPath)
	if err != nil {
		errs.AddPartial(1, fmt.Errorf(collectMetricError, metricName, err))
		return
	}
	s.mb.RecordMongodbSessionCountDataPoint(now, val)
}

// Admin Stats
func (s *mongodbScraper) recordOperations(now pcommon.Timestamp, doc bson.M, errs *scrapererror.ScrapeErrors) {
	for operationVal, operation := range metadata.MapAttributeOperation {
		metricPath := []string{"opcounters", operationVal}
		metricName := "mongodb.operation.count"
		val, err := collectMetric(doc, metricPath)
		if err != nil {
			errs.AddPartial(1, fmt.Errorf(collectMetricWithAttributes, metricName, operationVal, err))
			continue
		}
		s.mb.RecordMongodbOperationCountDataPoint(now, val, operation)
	}
}

func (s *mongodbScraper) recordCacheOperations(now pcommon.Timestamp, doc bson.M, errs *scrapererror.ScrapeErrors) {
	// Collect Cache Hits & Misses if wiredTiger storage engine is used
	// WiredTiger.cache metrics are available in 3.0+
	// https://www.mongodb.com/docs/v4.0/reference/command/serverStatus/#serverstatus.wiredTiger.cache
	mongo30, _ := version.NewVersion("3.0")
	if s.mongoVersion.LessThan(mongo30) {
		return
	}

	storageEngine, err := dig(doc, []string{"storageEngine", "name"})
	if err != nil {
		errs.AddPartial(1, errors.New("failed to find storage engine for cache operations"))
		return
	}
	if storageEngine != "wiredTiger" {
		// mongodb is using a different storage engine and this metric can not be collected
		return
	}

	metricPath := []string{"wiredTiger", "cache", "pages read into cache"}
	metricName := "mongodb.cache.operations"
	cacheMissVal, err := collectMetric(doc, metricPath)
	if err != nil {
		errs.AddPartial(2, fmt.Errorf(collectMetricWithAttributes, metricName, "miss, hit", err))
		return
	}
	s.mb.RecordMongodbCacheOperationsDataPoint(now, cacheMissVal, metadata.AttributeTypeMiss)

	cacheHitPath := []string{"wiredTiger", "cache", "pages requested from the cache"}
	cacheHitName := "mongodb.cache.operations"
	cacheHitVal, err := collectMetric(doc, cacheHitPath)
	if err != nil {
		errs.AddPartial(1, fmt.Errorf(collectMetricWithAttributes, cacheHitName, "hit", err))
		return
	}

	cacheHits := cacheHitVal - cacheMissVal
	s.mb.RecordMongodbCacheOperationsDataPoint(now, cacheHits, metadata.AttributeTypeHit)
}

func (s *mongodbScraper) recordGlobalLockTime(now pcommon.Timestamp, doc bson.M, errs *scrapererror.ScrapeErrors) {
	metricPath := []string{"globalLock", "totalTime"}
	metricName := "mongodb.global_lock.time"
	val, err := collectMetric(doc, metricPath)
	if err != nil {
		errs.AddPartial(1, fmt.Errorf(collectMetricError, metricName, err))
		return
	}
	heldTimeMilliseconds := val / 1000
	s.mb.RecordMongodbGlobalLockTimeDataPoint(now, heldTimeMilliseconds)
}

func (s *mongodbScraper) recordCursorCount(now pcommon.Timestamp, doc bson.M, errs *scrapererror.ScrapeErrors) {
	metricPath := []string{"metrics", "cursor", "open", "total"}
	metricName := "mongodb.cursor.count"
	val, err := collectMetric(doc, metricPath)
	if err != nil {
		errs.AddPartial(1, fmt.Errorf(collectMetricError, metricName, err))
		return
	}
	s.mb.RecordMongodbCursorCountDataPoint(now, val)
}

func (s *mongodbScraper) recordCursorTimeoutCount(now pcommon.Timestamp, doc bson.M, errs *scrapererror.ScrapeErrors) {
	metricPath := []string{"metrics", "cursor", "timedOut"}
	metricName := "mongodb.cursor.timeout.count"
	val, err := collectMetric(doc, metricPath)
	if err != nil {
		errs.AddPartial(1, fmt.Errorf(collectMetricError, metricName, err))
		return
	}
	s.mb.RecordMongodbCursorTimeoutCountDataPoint(now, val)
}

func (s *mongodbScraper) recordNetworkCount(now pcommon.Timestamp, doc bson.M, errs *scrapererror.ScrapeErrors) {
	networkRecorderMap := map[string]func(pcommon.Timestamp, int64){
		"bytesIn":     s.mb.RecordMongodbNetworkIoReceiveDataPoint,
		"bytesOut":    s.mb.RecordMongodbNetworkIoTransmitDataPoint,
		"numRequests": s.mb.RecordMongodbNetworkRequestCountDataPoint,
	}
	for networkKey, recorder := range networkRecorderMap {
		metricPath := []string{"network", networkKey}
		val, err := collectMetric(doc, metricPath)
		if err != nil {
			errs.AddPartial(1, fmt.Errorf(collectMetricError, networkKey, err))
			continue
		}
		recorder(now, val)
	}
}

// Lock Metrics are only supported by MongoDB v3.2+
func (s *mongodbScraper) recordLockAcquireCounts(now pcommon.Timestamp, doc bson.M, dBName string, errs *scrapererror.ScrapeErrors) {
	mongo32, _ := version.NewVersion("3.2")
	if s.mongoVersion.LessThan(mongo32) {
		return
	}
	mongo42, _ := version.NewVersion("4.2")
	for lockTypeKey, lockTypeAttribute := range lockTypeMap {
		for lockModeKey, lockModeAttribute := range lockModeMap {
			// Continue if the lock type is not supported by current server's MongoDB version
			if s.mongoVersion.LessThan(mongo42) && (lockTypeKey == "ParallelBatchWriterMode" || lockTypeKey == "ReplicationStateTransition") {
				continue
			}
			metricPath := []string{"locks", lockTypeKey, "acquireCount", lockModeKey}
			metricName := "mongodb.lock.acquire.count"
			metricAttributes := fmt.Sprintf("%s, %s, %s", dBName, lockTypeAttribute.String(), lockModeAttribute.String())
			val, err := collectMetric(doc, metricPath)
			// MongoDB only publishes this lock metric is it is available.
			// Do not raise error when key is not found
			if errors.Is(err, errKeyNotFound) {
				continue
			}
			if err != nil {
				errs.AddPartial(1, fmt.Errorf(collectMetricWithAttributes, metricName, metricAttributes, err))
				continue
			}
			s.mb.RecordMongodbLockAcquireCountDataPoint(now, val, dBName, lockTypeAttribute, lockModeAttribute)
		}
	}
}

func (s *mongodbScraper) recordLockAcquireWaitCounts(now pcommon.Timestamp, doc bson.M, dBName string, errs *scrapererror.ScrapeErrors) {
	mongo32, _ := version.NewVersion("3.2")
	if s.mongoVersion.LessThan(mongo32) {
		return
	}
	mongo42, _ := version.NewVersion("4.2")
	for lockTypeKey, lockTypeAttribute := range lockTypeMap {
		for lockModeKey, lockModeAttribute := range lockModeMap {
			// Continue if the lock type is not supported by current server's MongoDB version
			if s.mongoVersion.LessThan(mongo42) && (lockTypeKey == "ParallelBatchWriterMode" || lockTypeKey == "ReplicationStateTransition") {
				continue
			}
			metricPath := []string{"locks", lockTypeKey, "acquireWaitCount", lockModeKey}
			metricName := "mongodb.lock.acquire.wait_count"
			metricAttributes := fmt.Sprintf("%s, %s, %s", dBName, lockTypeAttribute.String(), lockModeAttribute.String())
			val, err := collectMetric(doc, metricPath)
			// MongoDB only publishes this lock metric is it is available.
			// Do not raise error when key is not found
			if errors.Is(err, errKeyNotFound) {
				continue
			}
			if err != nil {
				errs.AddPartial(1, fmt.Errorf(collectMetricWithAttributes, metricName, metricAttributes, err))
				continue
			}
			s.mb.RecordMongodbLockAcquireWaitCountDataPoint(now, val, dBName, lockTypeAttribute, lockModeAttribute)
		}
	}
}

func (s *mongodbScraper) recordLockTimeAcquiringMicros(now pcommon.Timestamp, doc bson.M, dBName string, errs *scrapererror.ScrapeErrors) {
	mongo32, _ := version.NewVersion("3.2")
	if s.mongoVersion.LessThan(mongo32) {
		return
	}
	mongo42, _ := version.NewVersion("4.2")
	for lockTypeKey, lockTypeAttribute := range lockTypeMap {
		for lockModeKey, lockModeAttribute := range lockModeMap {
			// Continue if the lock type is not supported by current server's MongoDB version
			if s.mongoVersion.LessThan(mongo42) && (lockTypeKey == "ParallelBatchWriterMode" || lockTypeKey == "ReplicationStateTransition") {
				continue
			}
			metricPath := []string{"locks", lockTypeKey, "timeAcquiringMicros", lockModeKey}
			metricName := "mongodb.lock.acquire.time"
			metricAttributes := fmt.Sprintf("%s, %s, %s", dBName, lockTypeAttribute.String(), lockModeAttribute.String())
			val, err := collectMetric(doc, metricPath)
			// MongoDB only publishes this lock metric is it is available.
			// Do not raise error when key is not found
			if errors.Is(err, errKeyNotFound) {
				continue
			}
			if err != nil {
				errs.AddPartial(1, fmt.Errorf(collectMetricWithAttributes, metricName, metricAttributes, err))
				continue
			}
			s.mb.RecordMongodbLockAcquireTimeDataPoint(now, val, dBName, lockTypeAttribute, lockModeAttribute)
		}
	}
}

func (s *mongodbScraper) recordLockDeadlockCount(now pcommon.Timestamp, doc bson.M, dBName string, errs *scrapererror.ScrapeErrors) {
	mongo32, _ := version.NewVersion("3.2")
	if s.mongoVersion.LessThan(mongo32) {
		return
	}
	mongo42, _ := version.NewVersion("4.2")
	for lockTypeKey, lockTypeAttribute := range lockTypeMap {
		for lockModeKey, lockModeAttribute := range lockModeMap {
			// Continue if the lock type is not supported by current server's MongoDB version
			if s.mongoVersion.LessThan(mongo42) && (lockTypeKey == "ParallelBatchWriterMode" || lockTypeKey == "ReplicationStateTransition") {
				continue
			}
			metricPath := []string{"locks", lockTypeKey, "deadlockCount", lockModeKey}
			metricName := "mongodb.lock.deadlock.count"
			metricAttributes := fmt.Sprintf("%s, %s, %s", dBName, lockTypeAttribute.String(), lockModeAttribute.String())
			val, err := collectMetric(doc, metricPath)
			// MongoDB only publishes this lock metric is it is available.
			// Do not raise error when key is not found
			if errors.Is(err, errKeyNotFound) {
				continue
			}
			if err != nil {
				errs.AddPartial(1, fmt.Errorf(collectMetricWithAttributes, metricName, metricAttributes, err))
				continue
			}
			s.mb.RecordMongodbLockDeadlockCountDataPoint(now, val, dBName, lockTypeAttribute, lockModeAttribute)
		}
	}
}

// Index Stats
func (s *mongodbScraper) recordIndexAccess(now pcommon.Timestamp, documents []bson.M, dbName string, collectionName string, errs *scrapererror.ScrapeErrors) {
	// Collect the index access given a collection and database if version is >= 3.2
	// https://www.mongodb.com/docs/v3.2/reference/operator/aggregation/indexStats/
	mongo32, _ := version.NewVersion("3.2")
	if s.mongoVersion.GreaterThanOrEqual(mongo32) {
		metricName := "mongodb.index.access.count"
		var indexAccessTotal int64
		for _, doc := range documents {
			metricAttributes := fmt.Sprintf("%s, %s", dbName, collectionName)
			indexAccess, ok := doc["accesses"].(bson.M)["ops"]
			if !ok {
				err := errors.New("could not find key for index access metric")
				errs.AddPartial(1, fmt.Errorf(collectMetricWithAttributes, metricName, metricAttributes, err))
				return
			}
			indexAccessValue, err := parseInt(indexAccess)
			if err != nil {
				errs.AddPartial(1, fmt.Errorf(collectMetricWithAttributes, metricName, metricAttributes, err))
				return
			}
			indexAccessTotal += indexAccessValue
		}
		s.mb.RecordMongodbIndexAccessCountDataPoint(now, indexAccessTotal, dbName, collectionName)
	}
}

// Top Stats
func (s *mongodbScraper) recordOperationTime(now pcommon.Timestamp, doc bson.M, errs *scrapererror.ScrapeErrors) {
	metricName := "mongodb.operation.time"
	collectionPathNames, err := digForCollectionPathNames(doc)
	if err != nil {
		errs.AddPartial(len(operationsMap), fmt.Errorf(collectMetricError, metricName, err))
		return
	}
	operationTimeValues, err := aggregateOperationTimeValues(doc, collectionPathNames, operationsMap)
	if err != nil {
		errs.AddPartial(len(operationsMap), fmt.Errorf(collectMetricError, metricName, err))
		return
	}

	for operationName, metadataOperationName := range operationsMap {
		operationValue, ok := operationTimeValues[operationName]
		if !ok {
			err := errors.New("could not find key for operation name")
			errs.AddPartial(1, fmt.Errorf(collectMetricWithAttributes, metricName, operationName, err))
			continue
		}
		s.mb.RecordMongodbOperationTimeDataPoint(now, operationValue, metadataOperationName)
	}
}

func aggregateOperationTimeValues(document bson.M, collectionPathNames []string, operationMap map[string]metadata.AttributeOperation) (map[string]int64, error) {
	operationTotals := map[string]int64{}
	for _, collectionPathName := range collectionPathNames {
		for operationName := range operationMap {
			value, err := getOperationTimeValues(document, collectionPathName, operationName)
			if err != nil {
				return nil, err
			}
			operationTotals[operationName] += value
		}
	}
	return operationTotals, nil
}

func getOperationTimeValues(document bson.M, collectionPathName, operation string) (int64, error) {
	rawValue, err := dig(document, []string{"totals", collectionPathName, operation, "time"})
	if err != nil {
		return 0, err
	}
	return parseInt(rawValue)
}

func digForCollectionPathNames(document bson.M) ([]string, error) {
	docTotals, ok := document["totals"].(bson.M)
	if !ok {
		return nil, errKeyNotFound
	}
	var collectionPathNames []string
	for collectionPathName := range docTotals {
		if collectionPathName != "note" {
			collectionPathNames = append(collectionPathNames, collectionPathName)
		}
	}
	return collectionPathNames, nil
}

func collectMetric(document bson.M, path []string) (int64, error) {
	metric, err := dig(document, path)
	if err != nil {
		return 0, err
	}
	return parseInt(metric)
}

func dig(document bson.M, path []string) (interface{}, error) {
	curItem, remainingPath := path[0], path[1:]
	value := document[curItem]
	if value == nil {
		return 0, errKeyNotFound
	}
	if len(remainingPath) == 0 {
		return value, nil
	}
	return dig(value.(bson.M), remainingPath)
}

func parseInt(val interface{}) (int64, error) {
	switch v := val.(type) {
	case int:
		return int64(v), nil
	case int32:
		return int64(v), nil
	case int64:
		return v, nil
	case float64:
		return int64(v), nil
	default:
		return 0, fmt.Errorf("could not parse value as int: %v", reflect.TypeOf(val))
	}
}
