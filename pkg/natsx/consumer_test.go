//go:build fast

package natsx

import (
	"testing"

	"github.com/primaryrutabaga/ruby-core/pkg/config"
)

func TestDefaultPullConsumerConfig(t *testing.T) {
	cfg := DefaultPullConsumerConfig("MY_STREAM", "my_durable", "ha.events.>")

	if cfg.Stream != "MY_STREAM" {
		t.Errorf("Stream = %q, want %q", cfg.Stream, "MY_STREAM")
	}
	if cfg.Durable != "my_durable" {
		t.Errorf("Durable = %q, want %q", cfg.Durable, "my_durable")
	}
	if cfg.FilterSubject != "ha.events.>" {
		t.Errorf("FilterSubject = %q, want %q", cfg.FilterSubject, "ha.events.>")
	}
	if cfg.MaxDeliver != config.DefaultMaxDeliver {
		t.Errorf("MaxDeliver = %d, want %d", cfg.MaxDeliver, config.DefaultMaxDeliver)
	}
	if cfg.MaxAckPending != config.DefaultMaxAckPending {
		t.Errorf("MaxAckPending = %d, want %d", cfg.MaxAckPending, config.DefaultMaxAckPending)
	}
	if cfg.AckWait != config.DefaultAckWait {
		t.Errorf("AckWait = %v, want %v", cfg.AckWait, config.DefaultAckWait)
	}
	if cfg.WorkerCount != config.DefaultWorkerCount {
		t.Errorf("WorkerCount = %d, want %d", cfg.WorkerCount, config.DefaultWorkerCount)
	}
	if cfg.FetchBatch != config.DefaultFetchBatch {
		t.Errorf("FetchBatch = %d, want %d", cfg.FetchBatch, config.DefaultFetchBatch)
	}
	if len(cfg.BackOff) != len(config.DefaultBackOff) {
		t.Errorf("BackOff len = %d, want %d", len(cfg.BackOff), len(config.DefaultBackOff))
	}
}

func TestDefaultPullConsumerConfig_FetchBatchNotExceedWorkerCount(t *testing.T) {
	cfg := DefaultPullConsumerConfig("STREAM", "durable", "subj.>")
	if cfg.FetchBatch > cfg.WorkerCount {
		t.Errorf("FetchBatch (%d) exceeds WorkerCount (%d); defaults violate ADR-0024 invariant",
			cfg.FetchBatch, cfg.WorkerCount)
	}
}

func TestEnsurePullConsumer_FetchBatchExceedsWorkerCount(t *testing.T) {
	cfg := DefaultPullConsumerConfig("STREAM", "durable", "subj.>")
	cfg.FetchBatch = cfg.WorkerCount + 1

	_, err := EnsurePullConsumer(nil, cfg)
	if err == nil {
		t.Fatal("expected error when FetchBatch > WorkerCount, got nil")
	}
}
