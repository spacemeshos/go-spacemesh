package grpcserver

import (
	"context"
	"testing"

	pb "github.com/spacemeshos/api/release/go/spacemesh/v1"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
	"go.uber.org/zap/zaptest"

	"github.com/spacemeshos/go-spacemesh/common/types"
)

func TestPostInfoService(t *testing.T) {
	log := zaptest.NewLogger(t)
	mpostStates := NewMockpostState(gomock.NewController(t))
	svc := NewPostInfoService(log, mpostStates)
	cfg, cleanup := launchServer(t, svc)
	t.Cleanup(cleanup)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	conn := dialGrpc(ctx, t, cfg)

	// create client
	client := pb.NewPostInfoServiceClient(conn)

	states := map[types.NodeID]int{types.RandomNodeID(): 0, types.RandomNodeID(): 1}
	mpostStates.EXPECT().PostStates().Return(states)
	resp, err := client.PostStates(ctx, &pb.PostStatesRequest{})
	require.NoError(t, err)
	require.NotNil(t, resp)

	for id, state := range states {
		require.Contains(t, resp.States, &pb.PostState{
			Id:    id.Bytes(),
			State: pb.PostState_State(state),
		})
	}
}
