package server

import (
	"net"
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
	"go.uber.org/zap/zaptest"

	"github.com/spacemeshos/go-spacemesh/activation"
	types "github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/p2p/pubsub/mocks"
)

func TestServerReadiness(t *testing.T) {
	ctrl := gomock.NewController(t)

	beacons := NewMockbeaconService(ctrl)
	srv := NewServer(
		activation.NewMockAtxService(ctrl),
		beacons,
		mocks.NewMockPublisher(ctrl),
		NewMockpoetDB(ctrl),
		NewMockhare(ctrl),
		NewMockproposalBuilder(ctrl),
		zaptest.NewLogger(t),
	)

	listener, err := net.Listen("tcp", "localhost:0")
	require.NoError(t, err)

	readiness := make(chan struct{})
	server := &http.Server{
		Handler: srv.IntoHandler(http.NewServeMux(), readiness),
	}

	go server.Serve(listener)
	defer server.Close()

	// Test that server returns 503 when not ready
	t.Run("returns 503 when not ready", func(t *testing.T) {
		resp, err := http.Get("http://" + listener.Addr().String() + "/beacon/1")
		require.NoError(t, err)
		require.Equal(t, http.StatusServiceUnavailable, resp.StatusCode)
	})

	// Test that server returns normal response after ready
	t.Run("succeeds after ready", func(t *testing.T) {
		close(readiness)

		beacons.EXPECT().
			Beacon(gomock.Any(), gomock.Any()).
			Return(types.Beacon{1, 2, 3}, nil)

		resp, err := http.Get("http://" + listener.Addr().String() + "/beacon/1")
		require.NoError(t, err)
		require.Equal(t, http.StatusOK, resp.StatusCode)
	})
}
