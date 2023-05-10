package p2p_test

import (
	"context"
	"encoding/binary"
	"testing"
	"time"

	pubsub "github.com/libp2p/go-libp2p-pubsub"
	pb "github.com/libp2p/go-libp2p-pubsub/pb"
	"github.com/libp2p/go-libp2p/core/network"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/libp2p/go-libp2p/core/protocol"
	varint "github.com/multiformats/go-varint"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"golang.org/x/sync/errgroup"

	"github.com/spacemeshos/go-spacemesh/cmd/node"
	"github.com/spacemeshos/go-spacemesh/config"
	"github.com/spacemeshos/go-spacemesh/events"
	"github.com/spacemeshos/go-spacemesh/log"
	ps "github.com/spacemeshos/go-spacemesh/p2p/pubsub"
)

func TestPeerDisconnectForMessageResultValidationReject(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Make 2 node instances
	conf1 := config.DefaultTestConfig()
	conf1.DataDirParent = t.TempDir()
	conf1.P2P.Listen = "/ip4/127.0.0.1/tcp/0"
	app1, err := NewApp(&conf1)
	require.NoError(t, err)
	conf2 := config.DefaultTestConfig()
	conf2.DataDirParent = t.TempDir()
	conf2.P2P.Listen = "/ip4/127.0.0.1/tcp/0"
	app2, err := NewApp(&conf2)
	require.NoError(t, err)
	t.Cleanup(func() {
		app1.Cleanup(ctx)
		app2.Cleanup(ctx)
	})
	g := errgroup.Group{}
	g.Go(func() error {
		return app1.Start(ctx)
	})
	<-app1.Started()
	g.Go(func() error {
		return app2.Start(ctx)
	})
	<-app2.Started()

	// Connect app2 to app1
	err = app2.Host().Connect(context.Background(), peer.AddrInfo{
		ID:    app1.Host().ID(),
		Addrs: app1.Host().Addrs(),
	})
	require.NoError(t, err)

	conns := app2.Host().Network().ConnsToPeer(app1.Host().ID())
	require.Equal(t, 1, len(conns))

	// Wait for streams to be established, one outbound and one inbound.
	require.Eventually(t, func() bool {
		return len(conns[0].GetStreams()) == 2
	}, time.Second*5, time.Millisecond*50)

	s := getStream(conns[0], pubsub.GossipSubID_v11, network.DirOutbound)

	// Create invalid atx message.
	protocol := ps.AtxProtocol
	m := &pb.Message{
		Data:  make([]byte, 20),
		Topic: &protocol,
	}
	// Send the invalid message.
	err = writeRpc(rpcWithMessages(m), s)
	require.NoError(t, err)

	require.Eventually(t, func() bool {
		return len(app2.Host().Network().ConnsToPeer(app1.Host().ID())) == 0
	}, time.Second*15, time.Millisecond*200)

	// Stop the nodes by canceling the context
	cancel()
	require.NoError(t, g.Wait())
}

func NewApp(conf *config.Config) (*node.App, error) {
	app := node.New(
		node.WithConfig(conf),
		node.WithLog(log.RegisterHooks(
			log.NewWithLevel("", zap.NewAtomicLevelAt(zapcore.DebugLevel)),
			events.EventHook())),
	)

	err := app.Initialize()
	return app, err
}

func getStream(c network.Conn, p protocol.ID, dir network.Direction) network.Stream {
	for _, s := range c.GetStreams() {
		if s.Protocol() == p && s.Stat().Direction == dir {
			return s
		}
	}
	return nil
}

func rpcWithMessages(msgs ...*pb.Message) *pubsub.RPC {
	return &pubsub.RPC{RPC: pb.RPC{Publish: msgs}}
}

func writeRpc(rpc *pubsub.RPC, s network.Stream) error {
	size := uint64(rpc.Size())

	buf := make([]byte, varint.UvarintSize(size)+int(size))

	n := binary.PutUvarint(buf, size)
	_, err := rpc.MarshalTo(buf[n:])
	if err != nil {
		return err
	}

	_, err = s.Write(buf)
	return err
}
