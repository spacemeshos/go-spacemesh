package addressbook

import (
	"context"
	"encoding/binary"
	"fmt"
	"io/ioutil"
	"math/rand"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/libp2p/go-libp2p/core/crypto"
	"github.com/libp2p/go-libp2p/core/peer"
	ma "github.com/multiformats/go-multiaddr"
	"github.com/stretchr/testify/require"

	"github.com/spacemeshos/go-spacemesh/log/logtest"
)

// genRandomInfo generates addrInfo with valid multiaddr.
func genRandomInfo(tb testing.TB, rng *rand.Rand) *AddrInfo {
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
	return &AddrInfo{IP: ip, ID: id, RawAddr: raw, addr: addr}
}

func TestAddrBook_EncodeDecode(t *testing.T) {
	t.Parallel()
	t.Run("valid file", func(t *testing.T) {
		t.Parallel()
		expected, tmpDir := initBookAndPersist(t)

		book := NewAddrBook(DefaultAddressBookConfigWithDataDir(tmpDir), logtest.New(t))
		for pid, info := range expected {
			found := book.Lookup(pid)
			require.Equal(t, info, found)
		}
	})
	t.Run("invalid data checksum", func(t *testing.T) {
		t.Parallel()
		expected, tmpDir := initBookAndPersist(t)

		path := filepath.Join(tmpDir, peersFileName)
		file, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
		require.NoError(t, err)
		_, err = file.WriteString(".")
		require.NoError(t, err)
		require.NoError(t, file.Close())

		book := NewAddrBook(DefaultAddressBookConfigWithDataDir(tmpDir), logtest.New(t))
		for pid := range expected {
			found := book.Lookup(pid)
			require.Nil(t, found)
		}
	})
	t.Run("empty file", func(t *testing.T) {
		t.Parallel()
		expected, tmpDir := initBookAndPersist(t)

		require.NoError(t, ioutil.WriteFile(filepath.Join(tmpDir, peersFileName), []byte{}, 0o666))
		book := NewAddrBook(DefaultAddressBookConfigWithDataDir(tmpDir), logtest.New(t))
		for pid := range expected {
			found := book.Lookup(pid)
			require.Nil(t, found)
		}
	})
}

func initBookAndPersist(t *testing.T) (map[peer.ID]*AddrInfo, string) {
	lg := logtest.New(t)
	tmpDir := t.TempDir()
	book := NewAddrBook(DefaultAddressBookConfigWithDataDir(tmpDir), lg)
	rng := rand.New(rand.NewSource(time.Now().Unix()))

	expected := map[peer.ID]*AddrInfo{}
	for i := 0; i < 50; i++ {
		info := genRandomInfo(t, rng)
		expected[info.ID] = info
		book.AddAddress(info, info)
	}
	for pid, info := range expected {
		require.Equal(t, info, book.Lookup(pid))
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	book.Persist(ctx)
	return expected, tmpDir
}
