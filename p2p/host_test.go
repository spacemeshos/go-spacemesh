package p2p

import (
	"context"
	"testing"

	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/stretchr/testify/require"

	"github.com/spacemeshos/go-spacemesh/log/logtest"
)

func TestPrologue(t *testing.T) {
	cfg1 := DefaultConfig()
	cfg1.DataDir = t.TempDir()
	cfg1.Listen = "/ip4/127.0.0.1/tcp/0"
	cfg2 := DefaultConfig()
	cfg2.DataDir = t.TempDir()
	cfg2.Listen = "/ip4/127.0.0.1/tcp/0"

	h1, err := New(context.Background(), logtest.New(t), cfg1, []byte("red"))
	require.NoError(t, err)
	h2, err := New(context.Background(), logtest.New(t), cfg2, []byte("blue"))
	require.NoError(t, err)
	err = h1.Connect(context.Background(), peer.AddrInfo{
		ID:    h2.ID(),
		Addrs: h2.Addrs(),
	})
	require.ErrorContains(t, err, "failed to negotiate security protocol")
}
