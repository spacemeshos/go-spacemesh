package p2p

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	mocknet "github.com/libp2p/go-libp2p/p2p/net/mock"
	"github.com/spacemeshos/go-spacemesh/log/logtest"
	"github.com/stretchr/testify/require"
	"golang.org/x/sync/errgroup"
)

func TestConnectedPersist(t *testing.T) {
	dir := t.TempDir()
	ctx, cancel := context.WithCancel(context.Background())
	const n = 3
	mock, err := mocknet.FullMeshConnected(n)
	require.NoError(t, err)
	var eg errgroup.Group
	eg.Go(func() error {
		persist(ctx, logtest.New(t), mock.Hosts()[0], dir, 100*time.Microsecond)
		return nil
	})
	require.Eventually(t, func() bool {
		_, err := os.Stat(filepath.Join(dir, connectedFile))
		return err == nil
	}, 3*time.Second, 50*time.Microsecond)
	cancel()
	eg.Wait()
	peers, err := loadPeers(dir)
	require.NoError(t, err)
	require.Len(t, peers, n-1)
}

func TestConnectedLoadEmpty(t *testing.T) {
	dir := t.TempDir()
	peers, err := loadPeers(dir)
	require.NoError(t, err)
	require.Empty(t, peers)
}

func TestConnectedBrokenCRC(t *testing.T) {
	dir := t.TempDir()
	ctx, cancel := context.WithCancel(context.Background())
	const n = 3
	mock, err := mocknet.FullMeshConnected(n)
	require.NoError(t, err)
	var eg errgroup.Group
	eg.Go(func() error {
		persist(ctx, logtest.New(t), mock.Hosts()[0], dir, 100*time.Microsecond)
		return nil
	})
	require.Eventually(t, func() bool {
		_, err := os.Stat(filepath.Join(dir, connectedFile))
		return err == nil
	}, 3*time.Second, 50*time.Microsecond)
	cancel()
	eg.Wait()
	f, err := os.OpenFile(filepath.Join(dir, connectedFile), os.O_RDWR, 0600)
	require.NoError(t, err)
	_, err = f.WriteAt([]byte{0, 1, 1, 1}, 0)
	require.NoError(t, err)
	peers, err := loadPeers(dir)
	require.ErrorContains(t, err, "invalid checksum")
	require.Empty(t, peers)
}
