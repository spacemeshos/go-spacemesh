package activation

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
	"go.uber.org/zap/zaptest"
	"golang.org/x/exp/maps"

	"github.com/spacemeshos/go-spacemesh/common/types"
)

func TestPostStates_RegisteredStartsIdle(t *testing.T) {
	tab := newTestBuilder(t, 3)

	states := tab.PostStates()
	require.Len(t, states, len(tab.signers))

	iDsInStates := make([]types.NodeID, 0)
	for desc, state := range states {
		require.Contains(t, tab.signers, desc.NodeID())
		require.Equal(t, types.PostStateIdle, state)
		iDsInStates = append(iDsInStates, desc.NodeID())
	}
	require.ElementsMatch(t, maps.Keys(tab.signers), iDsInStates)
}

func TestPostStates_SetState(t *testing.T) {
	postStates := NewPostStates(zaptest.NewLogger(t))
	id := types.RandomNodeID()

	postStates.Set(id, types.PostStateProving)
	states := postStates.Get()
	require.Len(t, states, 1)
	require.Equal(t, types.PostStateProving, states[id])

	postStates.Set(id, types.PostStateIdle)
	states = postStates.Get()
	require.Equal(t, types.PostStateIdle, states[id])
}

func TestPostState_OnProof(t *testing.T) {
	ctrl := gomock.NewController(t)

	mpostStates := NewMockPostStates(ctrl)
	mPostService := NewMockpostService(ctrl)
	mPostClient := NewMockPostClient(ctrl)
	nb, err := NewNIPostBuilder(
		nil,
		mPostService,
		zaptest.NewLogger(t),
		PoetConfig{},
		nil,
		nil,
		NipostbuilderWithPostStates(mpostStates),
	)
	require.NoError(t, err)

	id := types.RandomNodeID()

	mPostService.EXPECT().Client(id).Return(mPostClient, nil)
	mPostClient.EXPECT().Proof(gomock.Any(), gomock.Any()).Return(nil, nil, nil)
	gomock.InOrder(
		mpostStates.EXPECT().Set(id, types.PostStateProving),
		mpostStates.EXPECT().Set(id, types.PostStateIdle),
	)

	_, _, err = nb.Proof(context.Background(), id, []byte("abc"), nil)
	require.NoError(t, err)
}
