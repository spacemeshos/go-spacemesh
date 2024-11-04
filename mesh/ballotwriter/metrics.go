package ballotwriter

import (
	"github.com/prometheus/client_golang/prometheus"

	"github.com/spacemeshos/go-spacemesh/metrics"
)

const (
	namespace = "ballotwriter"
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

var LayerBallotTime = metrics.NewSimpleCounter(
	namespace,
	"layer_ballot_time",
	"time spent getting layer-ballot by node id (under lock)",
)

var BallotAddTime = metrics.NewSimpleCounter(
	namespace,
	"ballot_add_time",
	"time spent in ballots.Add (under lock)",
)

var CleanupTime = metrics.NewSimpleCounter(
	namespace,
	"cleanup_time",
	"time spent in final cleanup(under lock)",
)

var WriteTime = metrics.NewSimpleCounter(
	namespace,
	"batch_write_time",
	"time spent writing data to db (under lock)",
)

var WriteTimeHist = metrics.NewSimpleHistogram(
	"write_time_hist",
	namespace,
	"observed time (in seconds) for writing a ballot",
	prometheus.ExponentialBuckets(0.01, 10, 5),
)
