package metrics

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/spacemeshos/fixed"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/metrics"
)

const (
	subsystem   = "beacons"
	labelEpoch  = "epoch"
	labelBeacon = "beacon"
)

// BeaconStats hold metadata for each beacon value.
type BeaconStats struct {
	Epoch      types.EpochID
	Beacon     string
	Weight     fixed.Fixed
	WeightUnit int
}

// GatherCB returns stats for the observed and calculated beacons.
type GatherCB func() ([]*BeaconStats, *BeaconStats)

// BeaconMetricsCollector is a prometheus Collector for beacon metrics.
type BeaconMetricsCollector struct {
	gather GatherCB
	logger log.Logger

	registry               *prometheus.Registry
	observedBeaconCount    *prometheus.Desc
	observedBeaconWeight   *prometheus.Desc
	calculatedBeaconWeight *prometheus.Desc
}

var nameCalculatedWeight = prometheus.BuildFQName(metrics.Namespace, subsystem, "beacon_calculated_weight")

func MetricNameCalculatedWeight() string {
	return nameCalculatedWeight
}

// NewBeaconMetricsCollector creates a prometheus Collector for beacons.
func NewBeaconMetricsCollector(cb GatherCB, logger log.Logger) *BeaconMetricsCollector {
	bmc := &BeaconMetricsCollector{
		gather: cb,
		logger: logger,
		observedBeaconCount: prometheus.NewDesc(
			prometheus.BuildFQName(metrics.Namespace, subsystem, "beacon_observed_total"),
			"Number of beacons collected from blocks for each epoch and value",
			[]string{labelEpoch, labelBeacon}, nil),
		observedBeaconWeight: prometheus.NewDesc(
			prometheus.BuildFQName(metrics.Namespace, subsystem, "beacon_observed_weight"),
			"Weight of beacons collected from blocks for each epoch and value",
			[]string{labelEpoch, labelBeacon}, nil),
		calculatedBeaconWeight: prometheus.NewDesc(
			nameCalculatedWeight,
			"Weight of the beacon calculated by the node for each epoch",
			[]string{labelEpoch, labelBeacon}, nil),
	}
	return bmc
}

// Start registers the Collector with specified prometheus registry and starts the metrics collection.
func (bmc *BeaconMetricsCollector) Start(registry *prometheus.Registry) {
	if registry != nil {
		registry.MustRegister(bmc)
	} else {
		// use Register instead of MustRegister because during app test, multiple instances
		// will register the same set of metrics with the default registry and panic
		if err := prometheus.Register(bmc); err != nil {
			bmc.logger.With().Error("failed to register beacon metrics Collector", log.Err(err))
		}
	}
	bmc.registry = registry
}

// Stop unregisters the Collector with specified prometheus registry and stops the metrics collection.
func (bmc *BeaconMetricsCollector) Stop() {
	if bmc.registry != nil {
		bmc.registry.Unregister(bmc)
	} else {
		prometheus.Unregister(bmc)
	}
}

// Describe implements Collector.
func (bmc *BeaconMetricsCollector) Describe(ch chan<- *prometheus.Desc) {
	ch <- bmc.observedBeaconCount
	ch <- bmc.observedBeaconWeight
	ch <- bmc.calculatedBeaconWeight
}

// Collect implements Collector.
func (bmc *BeaconMetricsCollector) Collect(ch chan<- prometheus.Metric) {
	observed, calculated := bmc.gather()
	for _, ob := range observed {
		epochStr := ob.Epoch.String()
		ch <- prometheus.MustNewConstMetric(
			bmc.observedBeaconCount,
			prometheus.CounterValue,
			float64(ob.WeightUnit),
			epochStr,
			ob.Beacon,
		)
		ch <- prometheus.MustNewConstMetric(
			bmc.observedBeaconWeight,
			prometheus.CounterValue,
			ob.Weight.Float(),
			epochStr,
			ob.Beacon,
		)
	}

	if calculated == nil {
		return
	}
	// export the calculated beacon for the target epoch for ease of monitoring along with the observed beacons
	ch <- prometheus.MustNewConstMetric(
		bmc.calculatedBeaconWeight,
		prometheus.CounterValue,
		float64(0),
		calculated.Epoch.String(),
		calculated.Beacon,
	)
}

var NumMaliciousProps = metrics.NewCounter(
	"malicious_proposals",
	subsystem,
	"number of malicious proposals",
	[]string{},
).WithLabelValues()
