package meterstream

import (
	peer "gx/ipfs/QmXYjuNuxVzXKJCfWasQk1RqkhVLDM9jtUKhqc2WPQmFSB/go-libp2p-peer"
	protocol "gx/ipfs/QmZNkThpqfVXs9GNbexPrfBbXSLNYeKrE7jwFM2oqHbyqN/go-libp2p-protocol"
	inet "gx/ipfs/QmbD5yKbXahNvoMqzeuNyKQA9vAs9fUvJg2GXeWU1fVqY5/go-libp2p-net"
	metrics "gx/ipfs/QmbXmeK6KgUAkbyVGRxXknupmWAHnt6ryghT8BFSsEh2sB/go-libp2p-metrics"
)

type meteredStream struct {
	// keys for accessing metrics data
	protoKey protocol.ID
	peerKey  peer.ID

	inet.Stream

	// callbacks for reporting bandwidth usage
	mesSent metrics.StreamMeterCallback
	mesRecv metrics.StreamMeterCallback
}

func newMeteredStream(base inet.Stream, p peer.ID, recvCB, sentCB metrics.StreamMeterCallback) inet.Stream {
	return &meteredStream{
		Stream:   base,
		mesSent:  sentCB,
		mesRecv:  recvCB,
		protoKey: base.Protocol(),
		peerKey:  p,
	}
}

func WrapStream(base inet.Stream, bwc metrics.Reporter) inet.Stream {
	return newMeteredStream(base, base.Conn().RemotePeer(), bwc.LogRecvMessageStream, bwc.LogSentMessageStream)
}

func (s *meteredStream) Read(b []byte) (int, error) {
	n, err := s.Stream.Read(b)

	// Log bytes read
	s.mesRecv(int64(n), s.protoKey, s.peerKey)

	return n, err
}

func (s *meteredStream) Write(b []byte) (int, error) {
	n, err := s.Stream.Write(b)

	// Log bytes written
	s.mesSent(int64(n), s.protoKey, s.peerKey)

	return n, err
}
