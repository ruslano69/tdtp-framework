// Package event holds the wire shape tdtpcli publishes after a pipeline run
// (see pkg/resultlog), kept in its own leaf package deliberately: it has no
// dependency on pkg/etl or pkg/mercury.
//
// pkg/resultlog itself needs those — its RedisPublisher takes an
// etl.ResultLogConfig and an etl.ProcessorStats — but a consumer that only
// wants to read this JSON shape (the orchestrator's pub/sub trigger, for
// instance) has no reason to link tdtpcli's whole ETL engine into its own
// binary just to unmarshal one struct. pkg/etl is one Go package; importing
// any single symbol from it links every file in it, including
// kafka_spool.go (segmentio/kafka-go, ~40 subpackages), exporter.go and
// importer.go (pkg/brokers → RabbitMQ, pkg/xlsx → excelize), and
// config.go's own kanzi-go reference — none of which a plain HTTP+cron+SQLite
// service touches. Importing this package alone avoids all of that.
package event

import "time"

// PipelineResult is published to Redis after every tdtpcli --pipeline run
// configured with result_log.type: redis (see pkg/resultlog), whether it
// succeeded or not:
//
//	SET  tdtp:pipeline:<name>:state  <JSON>  EX <ttl>  — for polling
//	PUB  tdtp:pipeline:<name>        <JSON>             — for event-driven routing
type PipelineResult struct {
	PipelineName string    `json:"pipeline_name"`
	ResultName   string    `json:"result_name"`
	Status       string    `json:"status"`                 // "success" | "failed" | "completed_with_errors"
	PackageUUID  string    `json:"package_uuid,omitempty"` // UUID of the encrypted package (if encryption: true)
	StartedAt    time.Time `json:"started_at"`
	FinishedAt   time.Time `json:"finished_at"`
	DurationMs   int64     `json:"duration_ms"`
	RowsLoaded   int       `json:"rows_loaded"`
	RowsExported int       `json:"rows_exported"`
	Error        *string   `json:"error,omitempty"`
}
