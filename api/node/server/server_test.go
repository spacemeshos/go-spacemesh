package server

import (
	"net"
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
	"go.uber.org/zap/zaptest"
	"golang.org/x/sync/errgroup"

	"github.com/spacemeshos/go-spacemesh/activation"
	types "github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/p2p/pubsub/mocks"
	"github.com/spacemeshos/go-spacemesh/sql/statesql"
)

func TestServerReadiness(t *testing.T) {
	ctrl := gomock.NewController(t)

	beacons := NewMockbeaconService(ctrl)
	srv := NewServer(
		statesql.InMemoryTest(t),
		activation.NewMockAtxService(ctrl),
		beacons,
		mocks.NewMockPublisher(ctrl),
		NewMockpoetDB(ctrl),
		NewMockhare(ctrl),
		NewMockweights(ctrl),
		NewMockproposalBuilder(ctrl),
		zaptest.NewLogger(t),
	)

	listener, err := net.Listen("tcp", "localhost:0")
	require.NoError(t, err)

	syncer := NewMocksyncer(ctrl)
	server := &http.Server{
		Handler: srv.IntoHandler(http.NewServeMux(), syncer),
	}

	var eg errgroup.Group
	eg.Go(func() error {
		return server.Serve(listener)
	})
	t.Cleanup(func() {
		server.Close()
		eg.Wait()
	})

	t.Run("returns 503 when not ready", func(t *testing.T) {
		syncer.EXPECT().IsSynced(gomock.Any()).Return(false)
		resp, err := http.Get("http://" + listener.Addr().String() + "/beacon/1")
		require.NoError(t, err)
		require.Equal(t, http.StatusServiceUnavailable, resp.StatusCode)
	})

	t.Run("succeeds after ready", func(t *testing.T) {
		syncer.EXPECT().IsSynced(gomock.Any()).Return(true)
		beacons.EXPECT().
			Beacon(gomock.Any(), gomock.Any()).
			Return(types.Beacon{1, 2, 3}, nil)

		resp, err := http.Get("http://" + listener.Addr().String() + "/beacon/1")
		require.NoError(t, err)
		require.Equal(t, http.StatusOK, resp.StatusCode)
	})
}
