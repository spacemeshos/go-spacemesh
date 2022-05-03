package metrics_test

import (
	"testing"
	"time"

	"github.com/libp2p/go-libp2p-core/protocol"

	"github.com/golang/mock/gomock"

	"github.com/libp2p/go-libp2p-core/network"

	"github.com/spacemeshos/go-spacemesh/p2p/metrics"
	"github.com/spacemeshos/go-spacemesh/p2p/metrics/mocks"
)

//go:generate mockgen -package=mocks -destination=./mocks/mocks.go -source=./connections_test.go
type stream interface {
	network.Stream
}

func BenchmarkOpenCloseStream(b *testing.B) {
	conn := metrics.NewConnectionsMeeter()

	ctrl := gomock.NewController(b)
	opened := time.Now().Add(-1 * time.Minute)
	mockConn := mocks.NewMockstream(ctrl)
	mockConn.EXPECT().Protocol().Return(protocol.ID("test")).AnyTimes()
	mockConn.EXPECT().Stat().Return(network.Stat{Opened: opened}).AnyTimes()

	for i := 0; i < b.N; i++ {
		conn.OpenedStream(nil, mockConn)
		conn.ClosedStream(nil, mockConn)
	}
}

func BenchmarkConnectDisconnect(b *testing.B) {
	conn := metrics.NewConnectionsMeeter()
	for i := 0; i < b.N; i++ {
		conn.Connected(nil, nil)
		conn.Disconnected(nil, nil)
	}
}
