package activation

import (
	"context"
	"testing"
	"time"

	"github.com/spacemeshos/post/shared"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap/zaptest"
	"golang.org/x/exp/maps"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/events"
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
	postStates := newPostStates(zaptest.NewLogger(t))
	id := types.RandomNodeID()

	postStates.set(id, types.PostStateProving)
	states := postStates.get()
	require.Len(t, states, 1)
	require.Equal(t, types.PostStateProving, states[id])

	postStates.set(id, types.PostStateIdle)
	states = postStates.get()
	require.Equal(t, types.PostStateIdle, states[id])
}

func TestPostStates_ReactsOnEvents(t *testing.T) {
	postStates := newPostStates(zaptest.NewLogger(t))

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	require.NoError(t, postStates.watchEvents(ctx))

	ids := []types.NodeID{types.RandomNodeID(), types.RandomNodeID()}
	for _, id := range ids {
		postStates.set(id, types.PostStateIdle)
	}

	// Reacts to Post Start
	events.EmitPostStart(ids[0], shared.ZeroChallenge)
	require.Eventually(t, func() bool {
		return types.PostStateProving == postStates.get()[ids[0]]
	}, time.Second*5, time.Millisecond*100)

	states := postStates.get()
	for _, id := range ids[1:] {
		require.Contains(t, states, id)
		require.Equal(t, types.PostStateIdle, states[id])
	}

	// Reacts to Post Complete
	events.EmitPostComplete(ids[0], shared.ZeroChallenge)
	require.Eventually(t, func() bool {
		return types.PostStateIdle == postStates.get()[ids[0]]
	}, time.Second*5, time.Millisecond*100)
}
