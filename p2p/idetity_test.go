package p2p

import (
	"testing"

	"github.com/libp2p/go-libp2p/core/crypto"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/stretchr/testify/require"
)

func TestPersistedIdentity(t *testing.T) {
	dir := t.TempDir()
	_, err := EnsureIdentity(dir)
	require.NoError(t, err)

	info, err := identityInfoFromDir(dir)
	require.NoError(t, err)

	key, err := crypto.UnmarshalPrivateKey(info.Key)
	require.NoError(t, err)
	id, err := peer.IDFromPrivateKey(key)
	require.NoError(t, err)
	require.Equal(t, id, info.ID)
}
