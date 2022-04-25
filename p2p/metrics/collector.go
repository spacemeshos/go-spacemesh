package metrics

import (
	"github.com/prometheus/client_golang/prometheus"
)

type P2PMetricsCollector struct {
	totalPeers          *prometheus.Desc
	totalConnections    *prometheus.Desc
	totalSend           *prometheus.Desc
	totalRecv           *prometheus.Desc
	peersPerProtocol    *prometheus.Desc
	streamsPerProtocol  *prometheus.Desc
	trafficPerProtocol  *prometheus.Desc
	messagesPerProtocol *prometheus.Desc

	// traffic for each gossip protocol (protocol is a label, sent/received is a label), in bytes
	// total sent/received traffic for all protocols
	// number of messages for each gossip protocol (protocol is a label, sent/received is a label)
	// implementation of metrics.Reporter interface
	BandwidthReporter *BandwidthCollector
	// number of connections
	// number of streams per each protocol
	// histogram for server request durations for each protocol, counter inside
	// implementation of the network.Notifiee interface
	ConnectionsMeeter *ConnectionsMeeter
	// total number of peers
	// number of peers per each gossip protocol
	// implementation pubsub.RawTracer interface
	GoSipMeeter *GossipCollector
}

func NewNodeMetricsCollector() *P2PMetricsCollector {
	return &P2PMetricsCollector{
		BandwidthReporter: NewBandwidthCollector(),
		ConnectionsMeeter: NewConnectionsMeeter(),
		GoSipMeeter:       NewGoSIPCollector(),
		totalSend:         prometheus.NewDesc("p2p_total_send", "Total bytes sent by the node", nil, nil),
		totalRecv:         prometheus.NewDesc("p2p_total_recv", "Total bytes received by the node", nil, nil),
		totalPeers:        prometheus.NewDesc("p2p_total_peers", "Total number of peers", nil, nil),
		totalConnections:  prometheus.NewDesc("p2p_total_connections", "Total number of connections", nil, nil),
		peersPerProtocol: prometheus.NewDesc(
			"p2p_peers_per_protocol",
			"Number of peers per protocol",
			[]string{"protocol"},
			nil,
		),
		streamsPerProtocol: prometheus.NewDesc(
			"p2p_streams_per_protocol",
			"Number of streams per protocol",
			[]string{"protocol"},
			nil,
		),
		trafficPerProtocol: prometheus.NewDesc(
			"p2p_traffic_per_protocol",
			"Total bytes sent/received per protocol",
			[]string{"protocol", "direction"},
			nil,
		),
		messagesPerProtocol: prometheus.NewDesc(
			"p2p_messages_per_protocol",
			"Total messages sent/received per protocol",
			[]string{"protocol", "direction"},
			nil,
		),
	}
}

// Start registers the Collector and starts the metrics collection.
func (n *P2PMetricsCollector) Start() error {
	// use Register instead of MustRegister because during app test, multiple instances
	// will register the same set of metrics with the default registry and panic
	return prometheus.Register(n)
}

// Stop unregisters the Collector and stops the metrics collection.
func (n *P2PMetricsCollector) Stop() {
	prometheus.Unregister(n)
}

func (n *P2PMetricsCollector) Describe(descs chan<- *prometheus.Desc) {
	descs <- n.totalSend
	descs <- n.totalRecv
	descs <- n.totalPeers
	descs <- n.totalConnections
	descs <- n.peersPerProtocol
	descs <- n.streamsPerProtocol
	descs <- n.trafficPerProtocol
	descs <- n.messagesPerProtocol
}

func (n *P2PMetricsCollector) Collect(metrics chan<- prometheus.Metric) {
	gossip := n.GoSipMeeter.GetStat()
	bandwidth := n.BandwidthReporter.GetStat()
	connection := n.ConnectionsMeeter.GetStat()

	metrics <- prometheus.MustNewConstMetric(n.totalPeers, prometheus.GaugeValue, float64(gossip.TotalPeers))
	metrics <- prometheus.MustNewConstMetric(n.totalSend, prometheus.CounterValue, float64(bandwidth.TotalOut))
	metrics <- prometheus.MustNewConstMetric(n.totalRecv, prometheus.CounterValue, float64(bandwidth.TotalIn))
	metrics <- prometheus.MustNewConstMetric(n.totalConnections, prometheus.GaugeValue, float64(connection.TotalConnections))
	for proto, peers := range gossip.PeersPerProtocol {
		metrics <- prometheus.MustNewConstMetric(n.peersPerProtocol, prometheus.GaugeValue, float64(peers), string(proto))
	}
	for proto, streams := range connection.StreamsPerProtocol {
		metrics <- prometheus.MustNewConstMetric(n.streamsPerProtocol, prometheus.GaugeValue, float64(streams), string(proto))
	}
	for proto, traffic := range bandwidth.TrafficPerProtocol {
		metrics <- prometheus.MustNewConstMetric(n.trafficPerProtocol, prometheus.CounterValue, float64(traffic[outgoing]), string(proto), "out")
		metrics <- prometheus.MustNewConstMetric(n.trafficPerProtocol, prometheus.CounterValue, float64(traffic[incoming]), string(proto), "in")
	}
	for proto, messages := range bandwidth.MessagesPerProtocol {
		metrics <- prometheus.MustNewConstMetric(n.messagesPerProtocol, prometheus.CounterValue, float64(messages[outgoing]), string(proto), "out")
		metrics <- prometheus.MustNewConstMetric(n.messagesPerProtocol, prometheus.CounterValue, float64(messages[incoming]), string(proto), "in")
	}
}
