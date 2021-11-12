package peerexchange

import (
	"context"
	"encoding/binary"
	"fmt"
	"math/rand"
	"net"
	"testing"
	"time"

	"github.com/libp2p/go-libp2p-core/crypto"
	"github.com/libp2p/go-libp2p-core/peer"
	ma "github.com/multiformats/go-multiaddr"
	"github.com/stretchr/testify/require"

	"github.com/spacemeshos/go-spacemesh/log/logtest"
)

// genRandomInfo generates addrInfo with valid multiaddr.
func genRandomInfo(tb testing.TB, rng *rand.Rand) *addrInfo {
	tb.Helper()
	pk, _, err := crypto.GenerateEd25519Key(rng)
	require.NoError(tb, err)
	bytes, err := pk.Raw()
	require.NoError(tb, err)

	ip := net.IPv4(169, 255, bytes[0], bytes[1])
	port := binary.BigEndian.Uint64(bytes) % 65535
	id, err := peer.IDFromPrivateKey(pk)
	require.NoError(tb, err)

	raw := fmt.Sprintf("/ip4/%s/tcp/%d/p2p/%s", ip, port, id)
	addr, err := ma.NewMultiaddr(raw)
	require.NoError(tb, err)
	return &addrInfo{IP: ip, ID: id, RawAddr: raw, addr: addr}
}

func TestAddrBook_EncodeDecode(t *testing.T) {
	var (
		lg   = logtest.New(t)
		cfg  = Config{DataDir: t.TempDir()}
		book = newAddrBook(cfg, lg)
		rng  = rand.New(rand.NewSource(time.Now().Unix()))
	)

	expected := map[peer.ID]*addrInfo{}
	for i := 0; i < 50; i++ {
		info := genRandomInfo(t, rng)
		expected[info.ID] = info
		book.AddAddress(info, info)
	}
	for pid, info := range expected {
		found := book.Lookup(pid)
		require.Equal(t, info, found)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	book.Persist(ctx)

	book = newAddrBook(cfg, lg)
	for pid, info := range expected {
		found := book.Lookup(pid)
		require.Equal(t, info, found)
	}
}
