package server

import (
	"context"
	"io"
	"time"

	"github.com/libp2p/go-libp2p/core/connmgr"
	"github.com/libp2p/go-libp2p/core/network"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/libp2p/go-libp2p/core/protocol"
)

//go:generate mockgen -typed -package=mocks -destination=./mocks/mocks.go -source=./interface.go

// Host is a subset of libp2p Host interface that needs to be implemented to be usable with server.
type Host interface {
	SetStreamHandler(protocol.ID, network.StreamHandler)
	NewStream(context.Context, peer.ID, ...protocol.ID) (network.Stream, error)
	Network() network.Network
	ConnManager() connmgr.ConnManager
}

type peerStream interface {
	io.ReadWriteCloser
	SetDeadline(time.Time) error
}
