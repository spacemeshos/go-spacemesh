package v2beta1

import (
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
				_, err := svc.States(t.Context(), tc.req)
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

	broadcasted := identity.ATXBroadcasted{
		AtxId:   types.RandomATXID(),
		Publish: 9,
	}
	firstTimestamp := time.Now().Add(-time.Minute)
	states := map[time.Time]identity.State{
		firstTimestamp.Add(time.Second * 0): &identity.ATXReady{Publish: 9},
		firstTimestamp.Add(time.Second * 1): &broadcasted,
		firstTimestamp.Add(time.Second * 2): &identity.WaitForATXSynced{},
		firstTimestamp.Add(time.Second * 3): &identity.WaitForPoetRoundEnd{},
	}
	for t, s := range states {
		statesDB.SetAt(nodeID, s, t)
	}

	t.Run("filter by state", func(t *testing.T) {
		resp, err := svc.States(t.Context(), &pb.IdentityStatesRequest{
			States: []pb.IdentityState{pb.IdentityState_ATX_BROADCASTED},
			Limit:  10,
		})
		require.NoError(t, err)
		require.Len(t, resp.States, 1)
		require.Equal(t, *broadcasted.APIStateInfo().PublishEpoch, *resp.States[0].PublishEpoch)
		require.Equal(t, broadcasted.APIStateInfo().State, resp.States[0].State)
	})
	t.Run("DESC", func(t *testing.T) {
		resp, err := svc.States(t.Context(), &pb.IdentityStatesRequest{
			Order: pb.SortOrder_DESC,
			Limit: 10,
		})
		require.NoError(t, err)

		require.Len(t, resp.States, len(states))
		for i, timestamp := range slices.SortedFunc(maps.Keys(states), func(a, b time.Time) int {
			return b.Compare(a)
		}) {
			require.Equal(t, states[timestamp].APIStateInfo().State, resp.States[i].State)
		}
	})
	t.Run("ASC", func(t *testing.T) {
		resp, err := svc.States(t.Context(), &pb.IdentityStatesRequest{
			Order: pb.SortOrder_ASC,
			Limit: 10,
		})
		require.NoError(t, err)

		require.Len(t, resp.States, len(states))
		for i, timestamp := range slices.SortedFunc(maps.Keys(states), func(a, b time.Time) int {
			return a.Compare(b)
		}) {
			require.Equal(t, states[timestamp].APIStateInfo().State, resp.States[i].State)
		}
	})
	t.Run("from ASC", func(t *testing.T) {
		from := firstTimestamp.Add(2 * time.Second)
		resp, err := svc.States(t.Context(), &pb.IdentityStatesRequest{
			From:  timestamppb.New(from),
			Order: pb.SortOrder_ASC,
			Limit: 10,
		})
		require.NoError(t, err)

		require.Len(t, resp.States, 2)
		require.Equal(t, pb.IdentityState_WAIT_FOR_ATX_SYNCED, resp.States[0].State)
		require.Equal(t, pb.IdentityState_WAIT_FOR_POET_ROUND_END, resp.States[1].State)
	})
	t.Run("from DESC", func(t *testing.T) {
		from := firstTimestamp.Add(2 * time.Second)
		resp, err := svc.States(t.Context(), &pb.IdentityStatesRequest{
			From:  timestamppb.New(from),
			Order: pb.SortOrder_DESC,
			Limit: 10,
		})
		require.NoError(t, err)

		require.Len(t, resp.States, 2)
		require.Equal(t, pb.IdentityState_WAIT_FOR_POET_ROUND_END, resp.States[0].State)
		require.Equal(t, pb.IdentityState_WAIT_FOR_ATX_SYNCED, resp.States[1].State)
	})
	t.Run("from to ASC", func(t *testing.T) {
		from := firstTimestamp.Add(time.Second)
		to := from.Add(2 * time.Second)
		resp, err := svc.States(t.Context(), &pb.IdentityStatesRequest{
			From:  timestamppb.New(from),
			To:    timestamppb.New(to),
			Order: pb.SortOrder_ASC,
			Limit: 10,
		})
		require.NoError(t, err)

		require.Len(t, resp.States, 2)
		require.Equal(t, pb.IdentityState_ATX_BROADCASTED, resp.States[0].State)
		require.Equal(t, pb.IdentityState_WAIT_FOR_ATX_SYNCED, resp.States[1].State)
	})
	t.Run("from to DESC", func(t *testing.T) {
		from := firstTimestamp.Add(time.Second)
		to := from.Add(2 * time.Second)
		resp, err := svc.States(t.Context(), &pb.IdentityStatesRequest{
			From:  timestamppb.New(from),
			To:    timestamppb.New(to),
			Order: pb.SortOrder_DESC,
			Limit: 10,
		})
		require.NoError(t, err)

		require.Len(t, resp.States, 2)
		require.Equal(t, pb.IdentityState_WAIT_FOR_ATX_SYNCED, resp.States[0].State)
		require.Equal(t, pb.IdentityState_ATX_BROADCASTED, resp.States[1].State)
	})
}

func TestSmeshingIdentitiesService_FilterBySmeshers(t *testing.T) {
	statesDB := identity.NewIdentityStateStorage(localsql.InMemoryTest(t), zaptest.NewLogger(t))
	svc := NewSmeshingIdentitiesService(statesDB, nil, activation.PoetConfig{})

	timestamp := time.Now()
	nodeID := types.RandomNodeID()
	states := []identity.State{
		&identity.GeneratingPostProof{},
		&identity.PoetRegistered{},
	}
	for _, s := range states {
		statesDB.SetAt(nodeID, s, timestamp)
		timestamp = timestamp.Add(time.Second)
	}

	nodeID2 := types.RandomNodeID()
	states2 := []identity.State{
		&identity.Eligible{},
		&identity.GeneratingPostProof{},
	}
	for _, s := range states2 {
		statesDB.SetAt(nodeID2, s, timestamp)
		timestamp = timestamp.Add(time.Second)
	}
	nodeID3 := types.RandomNodeID()
	states3 := []identity.State{
		&identity.Retrying{},
		&identity.Retrying{},
		&identity.Retrying{},
	}
	for _, s := range states3 {
		statesDB.SetAt(nodeID3, s, timestamp)
		timestamp = timestamp.Add(time.Second)
	}

	resp, err := svc.States(t.Context(), &pb.IdentityStatesRequest{
		Smeshers: [][]byte{
			nodeID2[:],
			nodeID3[:],
		},
		Limit: 4,
	})
	require.NoError(t, err)
	require.Len(t, resp.States, 4)
	for i, state := range resp.States[:2] {
		require.Equal(t, states2[i].APIStateInfo().State, state.State)
	}
	for i, state := range resp.States[2:] {
		require.Equal(t, states3[i].APIStateInfo().State, state.State)
	}
}
