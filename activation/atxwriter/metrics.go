package atxwriter

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/spacemeshos/go-spacemesh/metrics"
)

const (
	namespace = "activation_write_coalescer"
)

var BatchWriteCount = metrics.NewSimpleCounter(
	namespace,
	"batch_write_count",
	"number of errors when writing a batch",
)

var WriteBatchErrorsCount = metrics.NewSimpleCounter(
	namespace,
	"write_batch_errors",
	"number of errors when writing a batch",
)

var ErroredBatchCount = metrics.NewSimpleCounter(
	namespace,
	"errored_batch",
	"number of batches that errored",
)

var FlushBatchSize = metrics.NewSimpleCounter(
	namespace,
	"flush_batch_size",
	"size of flushed batch",
)

var WriteTime = metrics.NewSimpleCounter(
	namespace,
	"write_time",
	"time spent writing the batch (not under lock)",
)

var AtxWriteTimeHist = metrics.NewHistogramNoLabel(
	"atx_write_time_hist",
	namespace,
	"time spent writing atxs in seconds (histogram)",
	prometheus.ExponentialBuckets(0.01, 10, 5),
)
