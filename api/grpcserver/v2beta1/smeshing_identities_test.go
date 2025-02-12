package v2beta1

import (
	"context"
	"maps"
	"math"
	"slices"
	"testing"
	"time"

	pb "github.com/spacemeshos/api/release/go/spacemesh/v2beta1"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap/zaptest"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/spacemeshos/go-spacemesh/activation"
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/identity"
	"github.com/spacemeshos/go-spacemesh/sql/localsql"
)

func TestSmeshingIdentitiesService_States(t *testing.T) {
	t.Run("request validation", func(t *testing.T) {
		states := identity.NewIdentityStateStorage(localsql.InMemoryTest(t), zaptest.NewLogger(t))
		svc := NewSmeshingIdentitiesService(states, nil, activation.PoetConfig{})
		tests := []struct {
			name    string
			req     *pb.IdentityStatesRequest
			wantErr codes.Code
		}{
			{
				name:    "limit exceeds maximum",
				req:     &pb.IdentityStatesRequest{Limit: 101},
				wantErr: codes.InvalidArgument,
			},
			{
				name:    "zero limit",
				req:     &pb.IdentityStatesRequest{Limit: 0},
				wantErr: codes.InvalidArgument,
			},
			{
				name: "invalid from timestamp",
				req: &pb.IdentityStatesRequest{
					Limit: 10,
					From:  &timestamppb.Timestamp{Seconds: math.MinInt64},
				},
				wantErr: codes.InvalidArgument,
			},
			{
				name: "invalid to timestamp",
				req: &pb.IdentityStatesRequest{
					Limit: 10,
					To:    &timestamppb.Timestamp{Seconds: math.MaxInt64},
				},
				wantErr: codes.InvalidArgument,
			},
		}

		for _, tc := range tests {
			t.Run(tc.name, func(t *testing.T) {
				_, err := svc.States(context.Background(), tc.req)
				st, ok := status.FromError(err)
				require.True(t, ok)
				require.Equal(t, tc.wantErr, st.Code())
			})
		}
	})
}

func TestSmeshingIdentitiesService_StatesFiltering(t *testing.T) {
	statesDB := identity.NewIdentityStateStorage(localsql.InMemoryTest(t), zaptest.NewLogger(t))
	svc := NewSmeshingIdentitiesService(statesDB, nil, activation.PoetConfig{})

	nodeID := types.RandomNodeID()

	firstTimestamp := time.Now().Add(-time.Minute)
	lastTimestamp := firstTimestamp
	nextTimestamp := func() time.Time {
		t := lastTimestamp
		lastTimestamp = lastTimestamp.Add(time.Second)
		return t
	}

	broadcasted := identity.ATXBroadcasted{
		AtxId:   types.RandomATXID(),
		Publish: 9,
	}
	states := map[time.Time]identity.State{
		nextTimestamp(): &identity.ATXReady{
			Publish: 9,
		},
		nextTimestamp(): &broadcasted,
		nextTimestamp(): &identity.WaitForATXSynced{},
		nextTimestamp(): &identity.WaitForPoetRoundEnd{},
	}
	for t, s := range states {
		statesDB.SetAt(nodeID, s, t)
	}

	t.Run("filter by state", func(t *testing.T) {
		resp, err := svc.States(context.Background(), &pb.IdentityStatesRequest{
			States: []pb.IdentityState{pb.IdentityState_ATX_BROADCASTED},
			Limit:  10,
		})
		require.NoError(t, err)
		require.Len(t, resp.Identities, 1)
		require.Len(t, resp.Identities[nodeID.String()].History, 1)

		gotState := resp.Identities[nodeID.String()].History[0]
		require.Equal(t, *broadcasted.APIStateInfo().PublishEpoch, *gotState.PublishEpoch)
		require.Equal(t, broadcasted.APIStateInfo().State, gotState.State)
	})
	t.Run("DESC", func(t *testing.T) {
		resp, err := svc.States(context.Background(), &pb.IdentityStatesRequest{
			Order: pb.SortOrder_DESC,
			Limit: 10,
		})
		require.NoError(t, err)
		require.Len(t, resp.Identities, 1)

		got := resp.Identities[nodeID.String()].History
		for i, timestamp := range slices.SortedFunc(maps.Keys(states), func(a, b time.Time) int {
			return b.Compare(a)
		}) {
			require.Equal(t, states[timestamp].APIStateInfo().State, got[i].State)
		}
	})
	t.Run("ASC", func(t *testing.T) {
		resp, err := svc.States(context.Background(), &pb.IdentityStatesRequest{
			Order: pb.SortOrder_ASC,
			Limit: 10,
		})
		require.NoError(t, err)
		require.Len(t, resp.Identities, 1)

		got := resp.Identities[nodeID.String()].History
		require.Len(t, got, len(states))
		for i, timestamp := range slices.SortedFunc(maps.Keys(states), func(a, b time.Time) int {
			return a.Compare(b)
		}) {
			require.Equal(t, states[timestamp].APIStateInfo().State, got[i].State)
		}
	})
	t.Run("from ASC", func(t *testing.T) {
		from := firstTimestamp.Add(2 * time.Second)
		resp, err := svc.States(context.Background(), &pb.IdentityStatesRequest{
			From:  timestamppb.New(from),
			Order: pb.SortOrder_ASC,
			Limit: 10,
		})
		require.NoError(t, err)
		require.Len(t, resp.Identities, 1)

		states := resp.Identities[nodeID.String()].History
		require.Len(t, states, 2)
		require.Equal(t, pb.IdentityState_WAIT_FOR_ATX_SYNCED, states[0].State)
		require.Equal(t, pb.IdentityState_WAIT_FOR_POET_ROUND_END, states[1].State)
	})
	t.Run("from DESC", func(t *testing.T) {
		from := firstTimestamp.Add(2 * time.Second)
		resp, err := svc.States(context.Background(), &pb.IdentityStatesRequest{
			From:  timestamppb.New(from),
			Order: pb.SortOrder_DESC,
			Limit: 10,
		})
		require.NoError(t, err)
		require.Len(t, resp.Identities, 1)

		states := resp.Identities[nodeID.String()].History
		require.Len(t, states, 3)
		require.Equal(t, pb.IdentityState_WAIT_FOR_ATX_SYNCED, states[0].State)
		require.Equal(t, pb.IdentityState_ATX_BROADCASTED, states[1].State)
		require.Equal(t, pb.IdentityState_ATX_READY, states[2].State)
	})
	t.Run("from to ASC", func(t *testing.T) {
		from := firstTimestamp.Add(2 * time.Second)
		to := from.Add(time.Second)
		resp, err := svc.States(context.Background(), &pb.IdentityStatesRequest{
			From:  timestamppb.New(from),
			To:    timestamppb.New(to),
			Order: pb.SortOrder_ASC,
			Limit: 10,
		})
		require.NoError(t, err)
		require.Len(t, resp.Identities, 1)

		states := resp.Identities[nodeID.String()].History
		require.Len(t, states, 1)
		require.Equal(t, pb.IdentityState_WAIT_FOR_ATX_SYNCED, states[0].State)
	})

	//
	// t.Run("time range DESC", func(t *testing.T) {
	// 	db := setupDB(t)
	// 	state := identities.New(db)
	// 	nodeID := types.NodeID{1}
	// 	now := time.Now().Truncate(time.Second)
	// 	from := now.Add(-time.Hour)
	// 	to := now
	//
	// 	// Add states: one before range, one in range, one after range
	// 	addIdentityState(t, db, nodeID, types.IdentityState(1), from.Add(-time.Minute))
	// 	addIdentityState(t, db, nodeID, types.IdentityState(2), from.Add(time.Minute))
	// 	addIdentityState(t, db, nodeID, types.IdentityState(3), to.Add(time.Minute))
	//
	// 	svc := NewSmeshingIdentitiesService(state, nil, activation.PoetConfig{})
	// 	resp, err := svc.States(context.Background(), &pb.IdentityStatesRequest{
	// 		From:  timestamppb.New(from),
	// 		To:    timestamppb.New(to),
	// 		Limit: 10,
	// 		Order: pb.SortOrder_DESC,
	// 	})
	// 	require.NoError(t, err)
	// 	require.Len(t, resp.Identities, 1)
	// })
	//
	// t.Run("response format", func(t *testing.T) {
	// 	db := setupDB(t)
	// 	state := identities.New(db)
	// 	nodeID := types.NodeID{1}
	// 	now := time.Now().Truncate(time.Second)
	// 	stateTime := now.Add(-time.Hour)
	//
	// 	addIdentityState(t, db, nodeID, types.IdentityState(1), stateTime)
	// 	addIdentityState(t, db, nodeID, types.IdentityState(2), now)
	//
	// 	svc := NewSmeshingIdentitiesService(state, nil, activation.PoetConfig{})
	// 	resp, err := svc.States(context.Background(), &pb.IdentityStatesRequest{
	// 		Limit: 10,
	// 	})
	// 	require.NoError(t, err)
	// 	require.Len(t, resp.Identities, 1)
	//
	// 	identity := resp.Identities[nodeID.String()]
	// 	require.NotNil(t, identity)
	// 	require.Len(t, identity.History, 2)
	//
	// 	// History should be in reverse chronological order
	// 	require.Equal(t, pb.IdentityState_REGISTERED, identity.History[0].State)
	// 	require.Equal(t, pb.IdentityState_REGISTERING, identity.History[1].State)
	// 	require.Equal(t, timestamppb.New(now), identity.History[0].Time)
	// 	require.Equal(t, timestamppb.New(stateTime), identity.History[1].Time)
	// })
}
