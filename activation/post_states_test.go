package activation

import (
	"context"
	"testing"
	"time"

	"github.com/spacemeshos/post/shared"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap/zaptest"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/events"
)

func TestPostStates_RegisteredStartsIdle(t *testing.T) {
	tab := newTestBuilder(t, 3)

	states := tab.PostStates()
	for id := range tab.signers {
		require.Contains(t, states, id)
		require.Equal(t, stateIdle, postState(states[id]))
	}
}

func TestPostStates_SetState(t *testing.T) {
	postStates := newPostStates(zaptest.NewLogger(t))
	id := types.RandomNodeID()

	postStates.set(id, stateProving)
	states := postStates.get()
	require.Len(t, states, 1)
	require.Equal(t, stateProving, postState(states[id]))

	postStates.set(id, stateIdle)
	states = postStates.get()
	require.Equal(t, stateIdle, postState(states[id]))
}

func TestPostStates_ReactsOnEvents(t *testing.T) {
	postStates := newPostStates(zaptest.NewLogger(t))

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	require.NoError(t, postStates.watchEvents(ctx))

	ids := []types.NodeID{types.RandomNodeID(), types.RandomNodeID()}
	for _, id := range ids {
		postStates.set(id, stateIdle)
	}

	// Reacts to Post Start
	events.EmitPostStart(ids[0], shared.ZeroChallenge)
	require.Eventually(t, func() bool {
		return stateProving == postState(postStates.get()[ids[0]])
	}, time.Second*5, time.Millisecond*100)

	states := postStates.get()
	for _, id := range ids[1:] {
		require.Contains(t, states, id)
		require.Equal(t, stateIdle, postState(states[id]))
	}

	// Reacts to Post Complete
	events.EmitPostComplete(ids[0], shared.ZeroChallenge)
	require.Eventually(t, func() bool {
		return stateIdle == postState(postStates.get()[ids[0]])
	}, time.Second*5, time.Millisecond*100)
}
