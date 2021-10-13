package peerexchange

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	mocknet "github.com/libp2p/go-libp2p/p2p/net/mock"
	"github.com/stretchr/testify/require"

	"github.com/spacemeshos/go-spacemesh/log/logtest"
)

func TestDiscovery_CrawlMesh(t *testing.T) {
	var (
		instances = []*Discovery{}
		bootnode  *addrInfo
		n         = 20
		rounds    = 5
	)
	mesh, err := mocknet.FullMeshLinked(context.TODO(), n)
	require.NoError(t, err)

	for _, h := range mesh.Hosts() {
		logger := logtest.New(t).Named(h.ID().Pretty())
		cfg := Config{}

		best, err := BestHostAddress(h)
		require.NoError(t, err)
		if bootnode == nil {
			bootnode = best
		} else {
			cfg.BootstrapNodes = append(cfg.BootstrapNodes, bootnode.RawAddr)
		}
		instance, err := New(logger, h, cfg)
		require.NoError(t, err)
		t.Cleanup(instance.Stop)
		instances = append(instances, instance)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var wg sync.WaitGroup
	for _, instance := range instances {
		wg.Add(1)
		go func(instance *Discovery) {
			defer wg.Done()
			for i := 0; i < rounds; i++ {
				if err := instance.Bootstrap(ctx); errors.Is(err, context.Canceled) {
					return
				}
				time.Sleep(10 * time.Millisecond)
			}
		}(instance)
	}
	wait := make(chan struct{})
	go func() {
		wg.Wait()
		close(wait)
	}()
	select {
	case <-time.After(5 * time.Second):
		require.FailNow(t, "bootstrap failed to finish on time")
	case <-wait:
	}
	for _, h := range mesh.Hosts() {
		require.True(t, len(h.Network().Conns()) > 1,
			"expected connections with more then just bootnode")
	}
}
