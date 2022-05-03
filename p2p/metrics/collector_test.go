package metrics_test

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/golang/mock/gomock"
	"github.com/libp2p/go-libp2p-core/network"
	"github.com/libp2p/go-libp2p-core/protocol"
	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/stretchr/testify/require"

	"github.com/spacemeshos/go-spacemesh/p2p/metrics"
	"github.com/spacemeshos/go-spacemesh/p2p/metrics/mocks"
)

type testMetric struct {
	metricType string
	metricHelp string
	metricData []string
}

type testOutput struct {
	data map[string]testMetric
}

func (t *testOutput) String() string {
	output := make([]string, 0, len(t.data))
	output = append(output, "")
	for key, v := range t.data {
		metricType := fmt.Sprintf("# TYPE %s %s", key, v.metricType)
		metricHelp := fmt.Sprintf("# HELP %s %s", key, v.metricHelp)
		output = append(output, fmt.Sprintf("%s\n%s\n%s", metricType, metricHelp, strings.Join(v.metricData, "\n")))
	}
	output = append(output, "")
	return strings.Join(output, "\n")
}

func TestP2PMetricsCollector(t *testing.T) {
	ctrl := gomock.NewController(t)
	mockConn := mocks.NewMockstream(ctrl)
	opened := time.Now().Add(-1 * time.Minute)
	mockConn.EXPECT().Protocol().Return(protocol.ID("test")).AnyTimes()
	mockConn.EXPECT().Stat().Return(network.Stat{Opened: opened}).AnyTimes()

	collector := metrics.NewNodeMetricsCollector()
	require.NoError(t, collector.Start())
	t.Cleanup(collector.Stop)
	// populate counters with seed data
	collector.ConnectionsMeeter.Connected(nil, nil)
	collector.ConnectionsMeeter.Connected(nil, nil)
	collector.ConnectionsMeeter.OpenedStream(nil, mockConn)
	collector.GoSipMeeter.AddPeer("peer_1", "proto_1")
	collector.GoSipMeeter.AddPeer("peer_1", "proto_2")
	collector.GoSipMeeter.AddPeer("peer_2", "proto_1")
	collector.BandwidthReporter.LogSentMessageStream(100, "proto_1", "peer_1")
	collector.BandwidthReporter.LogSentMessageStream(100, "proto_2", "peer_1")
	collector.BandwidthReporter.LogRecvMessageStream(100, "proto_2", "peer_1")
	collector.ConnectionsMeeter.ClosedStream(nil, mockConn)
	collector.ConnectionsMeeter.ClosedStream(nil, mockConn)

	data := testOutput{
		data: map[string]testMetric{
			"p2p_total_peers": {
				metricType: "gauge",
				metricHelp: "Total number of peers",
				metricData: []string{
					"p2p_total_peers 2",
				},
			},
			"p2p_total_send": {
				metricType: "counter",
				metricHelp: "Total bytes sent by the node",
				metricData: []string{
					"p2p_total_send 0",
				},
			},
			"p2p_total_recv": {
				metricType: "counter",
				metricHelp: "Total bytes received by the node",
				metricData: []string{
					"p2p_total_recv 0",
				},
			},
			"p2p_total_connections": {
				metricType: "gauge",
				metricHelp: "Total number of connections",
				metricData: []string{
					"p2p_total_connections 2",
				},
			},
			"p2p_peers_per_protocol": {
				metricType: "gauge",
				metricHelp: "Number of peers per protocol",
				metricData: []string{
					`p2p_peers_per_protocol{protocol="proto_1"} 2`,
					`p2p_peers_per_protocol{protocol="proto_2"} 1`,
				},
			},
			"p2p_streams_per_protocol": {
				metricType: "gauge",
				metricHelp: "Number of streams per protocol",
				metricData: []string{
					`p2p_streams_per_protocol{protocol="test"} 0`,
				},
			},
			"p2p_messages_per_protocol": {
				metricType: "counter",
				metricHelp: "Total messages sent/received per protocol",
				metricData: []string{
					`p2p_messages_per_protocol{direction="in",protocol="proto_1"} 0`,
					`p2p_messages_per_protocol{direction="in",protocol="proto_2"} 1`,
					`p2p_messages_per_protocol{direction="out",protocol="proto_1"} 1`,
					`p2p_messages_per_protocol{direction="out",protocol="proto_2"} 1`,
				},
			},
			"p2p_traffic_per_protocol": {
				metricType: "counter",
				metricHelp: "Total bytes sent/received per protocol",
				metricData: []string{
					`p2p_traffic_per_protocol{direction="in",protocol="proto_1"} 0`,
					`p2p_traffic_per_protocol{direction="in",protocol="proto_2"} 0`,
					`p2p_traffic_per_protocol{direction="out",protocol="proto_1"} 0`,
					`p2p_traffic_per_protocol{direction="out",protocol="proto_2"} 0`,
				},
			},
		},
	}

	err := testutil.CollectAndCompare(collector, strings.NewReader(data.String()))
	require.NoError(t, err)
}
