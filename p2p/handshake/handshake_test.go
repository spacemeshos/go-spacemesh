package handshake

import (
	"context"
	"testing"

	mocknet "github.com/libp2p/go-libp2p/p2p/net/mock"
	"github.com/spacemeshos/go-scale/tester"
	"github.com/stretchr/testify/require"

	"github.com/spacemeshos/go-spacemesh/common/types"
)

func TestHandshake(t *testing.T) {
	mesh, err := mocknet.FullMeshConnected(3)
	require.NoError(t, err)
	genesisID := types.Hash20{48, 29, 252, 229, 229, 174, 157, 108, 84, 53, 111, 208, 231, 2, 102, 52, 98, 247, 61, 190}
	hs1 := New(mesh.Hosts()[0], genesisID)
	t.Cleanup(hs1.Stop)
	hs2 := New(mesh.Hosts()[1], genesisID)
	t.Cleanup(hs2.Stop)
	hs3 := New(mesh.Hosts()[2], types.Hash20{})
	t.Cleanup(hs3.Stop)

	require.NoError(t, hs1.Request(context.TODO(), hs2.h.ID()))
	require.Error(t, hs1.Request(context.TODO(), hs3.h.ID()))
}

func FuzzHandshakeAckConsistency(f *testing.F) {
	tester.FuzzConsistency[HandshakeAck](f)
}

func FuzzHandshakeAckSafety(f *testing.F) {
	tester.FuzzSafety[HandshakeAck](f)
}

func FuzzHandshakeMessageConsistency(f *testing.F) {
	tester.FuzzConsistency[HandshakeMessage](f)
}

func FuzzHandshakeMessageSafety(f *testing.F) {
	tester.FuzzSafety[HandshakeMessage](f)
}
