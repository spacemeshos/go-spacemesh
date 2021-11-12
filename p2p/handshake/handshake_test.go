package handshake

import (
	"context"
	"testing"

	mocknet "github.com/libp2p/go-libp2p/p2p/net/mock"
	"github.com/stretchr/testify/require"
)

func TestHandshake(t *testing.T) {
	mesh, err := mocknet.FullMeshConnected(context.TODO(), 3)
	require.NoError(t, err)
	const netid = 1001
	hs1 := New(mesh.Hosts()[0], netid)
	t.Cleanup(hs1.Stop)
	hs2 := New(mesh.Hosts()[1], netid)
	t.Cleanup(hs2.Stop)
	hs3 := New(mesh.Hosts()[2], netid+1)
	t.Cleanup(hs3.Stop)

	require.NoError(t, hs1.Request(context.TODO(), hs2.h.ID()))
	require.Error(t, hs1.Request(context.TODO(), hs3.h.ID()))
}
