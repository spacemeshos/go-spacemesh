package grpcserver

import (
	"context"
	"testing"

	pb "github.com/spacemeshos/api/release/go/spacemesh/v1"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"github.com/spacemeshos/go-spacemesh/common/types"
)

type idMock struct {
	id   types.NodeID
	name string
}

func newIdMock(name string) idMock {
	return idMock{
		id:   types.RandomNodeID(),
		name: name,
	}
}

func (i idMock) NodeID() types.NodeID {
	return i.id
}

func (i idMock) Name() string {
	return i.name
}

func TestPostInfoService(t *testing.T) {
	mpostStates := NewMockpostState(gomock.NewController(t))
	svc := NewPostInfoService(mpostStates)
	cfg, cleanup := launchServer(t, svc)
	t.Cleanup(cleanup)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	conn := dialGrpc(t, cfg)
	client := pb.NewPostInfoServiceClient(conn)

	existingStates := map[types.IdentityDescriptor]types.PostState{
		newIdMock("idle.key"):    types.PostStateIdle,
		newIdMock("proving.key"): types.PostStateProving,
	}
	mpostStates.EXPECT().PostStates().Return(existingStates)

	resp, err := client.PostStates(ctx, &pb.PostStatesRequest{})
	require.NoError(t, err)
	require.NotNil(t, resp)

	for id, state := range existingStates {
		require.Contains(t, resp.States, &pb.PostState{
			Id:    id.NodeID().Bytes(),
			Name:  id.Name(),
			State: statusMap[state],
		})
	}
}
