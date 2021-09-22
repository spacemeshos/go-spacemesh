package metrics

import (
	"fmt"
	"net/http"
	"time"

	prmkit "github.com/go-kit/kit/metrics/prometheus"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/prometheus/client_golang/prometheus/push"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/log"
)

// Metrics labels.
const (
	LayerIDLabel   = "layer_id"
	BlockIDLabel   = "block_id"
	LastLayerLabel = "last_layer"
)

// Metrics defines an interface for metrics.
type Metrics interface {
	StartPushingMetrics(url string, periodSec int, nodeID, networkID string)
	StartMetricsServer(metricsPort int)
	LayerBlockSize(block types.Block)
	NumTxsInBlock(block types.Block)
	BaseBlockExceptionForLength(block types.Block)
	BaseBlockExceptionAgainstLength(block types.Block)
	BaseBlockExceptionNeutralLength(block types.Block)
	LayerDistanceToBaseBlock(lastLayer, baseBlockLayer types.LayerID, blockID types.BlockID)
	LayerNumBlocks(layer types.LayerID, blockIDs []types.BlockID)
	BlockBuildDuration(layerID types.LayerID, blockID types.BlockID, duration time.Duration)
}

// Prometheus is used for working with Prometheus metrics.
type Prometheus struct {
	logger log.Log
}

// NewPrometheus returns a new Prometheus.
func NewPrometheus(logger log.Log) *Prometheus {
	return &Prometheus{
		logger: logger,
	}
}

// StartPushingMetrics begins pushing metrics to the url specified by the --metrics-push flag
// with period specified by the --metrics-push-period flag
func (p *Prometheus) StartPushingMetrics(url string, periodSec int, nodeID, networkID string) {
	period := time.Duration(periodSec) * time.Second
	ticker := time.NewTicker(period)

	pusher := push.New(url, "go-spacemesh").Gatherer(prometheus.DefaultGatherer).
		Grouping("node_id", nodeID).
		Grouping("network_id", networkID)

	go func() {
		for range ticker.C {
			err := pusher.Push()
			if err != nil {
				p.logger.With().Warning("failed to push metrics", log.Err(err))
			}
		}
	}()
}

// StartMetricsServer begins listening and supplying metrics on localhost:`metricsPort`/metrics
func (p *Prometheus) StartMetricsServer(metricsPort int) {
	http.Handle("/metrics", promhttp.Handler())
	go func() {
		err := http.ListenAndServe(fmt.Sprintf(":%v", metricsPort), nil)
		p.logger.With().Warning("Metrics server stopped: %v", log.Err(err))
	}()
}

// LayerBlockSize sends 'average block size' metric.
func (p *Prometheus) LayerBlockSize(block types.Block) {
	opts := prometheus.HistogramOpts{
		Namespace: Namespace,
		Subsystem: "blocks",
		Name:      "layer_block_size",
		Help:      "Block size in bytes",
		Buckets:   prometheus.ExponentialBuckets(100, 2, 8),
	}

	labels := []string{
		LayerIDLabel,
		BlockIDLabel,
	}

	prmkit.NewHistogramFrom(opts, labels).
		With(LayerIDLabel, block.LayerIndex.String()).
		With(BlockIDLabel, block.ID().String()).
		Observe(float64(len(block.Bytes())))
}

// NumTxsInBlock sends 'number of transaction in block' metric.
func (p *Prometheus) NumTxsInBlock(block types.Block) {
	opts := prometheus.HistogramOpts{
		Namespace: Namespace,
		Subsystem: "blocks",
		Name:      "num_txs_in_block",
		Help:      "Number of transactions in block",
		Buckets:   prometheus.ExponentialBuckets(1, 2, 8),
	}

	labels := []string{
		LayerIDLabel,
		BlockIDLabel,
	}

	prmkit.NewHistogramFrom(opts, labels).
		With(LayerIDLabel, block.LayerIndex.String()).
		With(BlockIDLabel, block.ID().String()).
		Observe(float64(len(block.TxIDs)))
}

// BaseBlockExceptionForLength sends 'Base block exception length' (for) metric.
func (p *Prometheus) BaseBlockExceptionForLength(block types.Block) {
	opts := prometheus.HistogramOpts{
		Namespace: Namespace,
		Subsystem: "blocks",
		Name:      "base_block_exception_for_length",
		Help:      "Base block exception length (for)",
		Buckets:   prometheus.ExponentialBuckets(1, 2, 8),
	}

	labels := []string{
		LayerIDLabel,
		BlockIDLabel,
	}

	prmkit.NewHistogramFrom(opts, labels).
		With(LayerIDLabel, block.LayerIndex.String()).
		With(BlockIDLabel, block.ID().String()).
		Observe(float64(len(block.ForDiff)))
}

// BaseBlockExceptionAgainstLength sends 'Base block exception length' (against) metric.
func (p *Prometheus) BaseBlockExceptionAgainstLength(block types.Block) {
	opts := prometheus.HistogramOpts{
		Namespace: Namespace,
		Subsystem: "blocks",
		Name:      "base_block_exception_against_length",
		Help:      "Base block exception length (against)",
		Buckets:   prometheus.ExponentialBuckets(1, 2, 8),
	}

	labels := []string{
		LayerIDLabel,
		BlockIDLabel,
	}

	prmkit.NewHistogramFrom(opts, labels).
		With(LayerIDLabel, block.LayerIndex.String()).
		With(BlockIDLabel, block.ID().String()).
		Observe(float64(len(block.AgainstDiff)))
}

// BaseBlockExceptionNeutralLength sends 'Base block exception length' (neutral) metric.
func (p *Prometheus) BaseBlockExceptionNeutralLength(block types.Block) {
	opts := prometheus.HistogramOpts{
		Namespace: Namespace,
		Subsystem: "blocks",
		Name:      "base_block_exception_neutral_length",
		Help:      "Base block exception length (neutral)",
		Buckets:   prometheus.ExponentialBuckets(1, 2, 8),
	}

	labels := []string{
		LayerIDLabel,
		BlockIDLabel,
	}

	prmkit.NewHistogramFrom(opts, labels).
		With(LayerIDLabel, block.LayerIndex.String()).
		With(BlockIDLabel, block.ID().String()).
		Observe(float64(len(block.NeutralDiff)))
}

// LayerDistanceToBaseBlock send 'layer distance to base block' metric.
func (p *Prometheus) LayerDistanceToBaseBlock(lastLayer, baseBlockLayer types.LayerID, blockID types.BlockID) {
	opts := prometheus.HistogramOpts{
		Namespace: Namespace,
		Subsystem: "tortoise",
		Name:      "layer_distance_to_base_block",
		Help:      "How far back a node needs to find a good block",
		Buckets:   prometheus.ExponentialBuckets(1, 2, 16),
	}

	labels := []string{
		LastLayerLabel,
		BlockIDLabel,
	}

	prmkit.NewHistogramFrom(opts, labels).
		With(LastLayerLabel, lastLayer.String()).
		With(BlockIDLabel, blockID.String()).
		Observe(float64(lastLayer.Value - baseBlockLayer.Value))
}

// LayerNumBlocks sends 'number of blocks in layer metric'.
func (p *Prometheus) LayerNumBlocks(layer types.LayerID, blockIDs []types.BlockID) {
	opts := prometheus.HistogramOpts{
		Namespace: Namespace,
		Subsystem: "mesh",
		Name:      "layer_num_blocks",
		Help:      "Number of blocks in layer",
		Buckets:   prometheus.ExponentialBuckets(1, 2, 16),
	}

	labels := []string{
		LayerIDLabel,
	}

	prmkit.NewHistogramFrom(opts, labels).
		With(LayerIDLabel, layer.String()).
		Observe(float64(len(blockIDs)))
}

// BlockBuildDuration sends 'block build duration' (milliseconds) metric.
func (p *Prometheus) BlockBuildDuration(layerID types.LayerID, blockID types.BlockID, duration time.Duration) {
	opts := prometheus.HistogramOpts{
		Namespace: Namespace,
		Subsystem: "miner",
		Name:      "block_build_duration",
		Help:      "How long it takes to build a block (milliseconds)",
		Buckets:   []float64{10, 100, 1000, 5 * 1000, 10 * 1000, 60 * 1000, 10 * 60 * 1000, 60 * 60 * 1000},
	}

	labels := []string{
		LayerIDLabel,
		BlockIDLabel,
	}

	prmkit.NewHistogramFrom(opts, labels).
		With(LayerIDLabel, layerID.String()).
		With(BlockIDLabel, blockID.String()).
		Observe(float64(duration / time.Millisecond))
}
