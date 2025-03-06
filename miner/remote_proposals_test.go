package miner

import (
	"context"
	"math/rand"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
	"go.uber.org/zap/zaptest"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/miner/mocks"
	pmocks "github.com/spacemeshos/go-spacemesh/p2p/pubsub/mocks"
	"github.com/spacemeshos/go-spacemesh/signing"
)

func TestRemoteProposals(t *testing.T) {
	signers := make([]*signing.EdSigner, 4)
	rng := rand.New(rand.NewSource(10101))
	for i := range signers {
		signer, err := signing.NewEdSigner(signing.WithKeyFromRand(rng))
		require.NoError(t, err)
		signers[i] = signer
	}
	var (
		ctx, cancel = context.WithCancel(t.Context())
		ctrl        = gomock.NewController(t)
		clock       = mocks.NewMocklayerClock(ctrl)
		publisher   = pmocks.NewMockPublisher(ctrl)
		beacon      = mocks.NewMockbeaconService(ctrl)
		prop        = mocks.NewMockproposalService(ctrl)
		beaconVal   = types.Beacon{1}
		idStates    = mocks.NewMockidentityStates(ctrl)
	)

	clock.EXPECT().LayerToTime(gomock.Any()).Return(time.Unix(0, 0)).AnyTimes()

	builder := NewRemoteBuilder(clock, publisher, beacon, prop, 5, layersPerEpoch, zaptest.NewLogger(t), idStates)
	for _, signer := range signers {
		builder.Register(signer)
	}
	var (
		activeSet = types.ATXIDList{types.ATXID{1}}
		lid       = types.LayerID(1)
		meshHash  = types.Hash32{11}
		atxId     = types.ATXID{1}
		txIds     = []types.TransactionID{{1}}
		nrTicks   = 10
		ticks     = make(chan struct{}, nrTicks)
		closed    = make(chan struct{})
		done      = make(chan struct{})
	)
	close(closed)
	clock.EXPECT().CurrentLayer().DoAndReturn(func() types.LayerID {
		return lid
	}).AnyTimes()
	clock.EXPECT().AwaitLayer(gomock.Any()).DoAndReturn(func(l types.LayerID) <-chan struct{} {
		lid = l
		select {
		case ticks <- struct{}{}:
			return closed
		default:
			close(done)
			return make(chan struct{})
		}
	}).AnyTimes()
	beacon.EXPECT().Beacon(gomock.Any(), gomock.Any()).Return(types.Beacon{1}, nil).AnyTimes()
	publisher.EXPECT().Publish(gomock.Any(), gomock.Any(), gomock.Any()).Return(nil).Times(2)
	prop.EXPECT().Proposal(gomock.Any(), gomock.Any(), gomock.Any()).DoAndReturn(
		func(_ context.Context, lid types.LayerID, nodeId types.NodeID) (*types.Proposal, uint64, error) {
			prop := createTestProposal(t, activeSet, lid, meshHash, atxId, nodeId, txIds, beaconVal, 1)
			return prop, 11, nil
		}).AnyTimes()
	prop.EXPECT().CalculateEligibilitySlotsFor(gomock.Any(), gomock.Any(), gomock.Any()).DoAndReturn(
		func(_ context.Context, nodeId types.NodeID, epoch types.EpochID) (uint32, types.VRFPostIndex, error) {
			return 1, 11, nil
		}).AnyTimes()
	idStates.EXPECT().SetEligibilities(gomock.Any(), gomock.Any()).AnyTimes()
	idStates.EXPECT().AddProposal(gomock.Any(), gomock.Any()).AnyTimes()
	idStates.EXPECT().Set(gomock.Any(), gomock.Any()).AnyTimes()
	go builder.Run(ctx)
	defer cancel()
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("test timeout")
	}
}

func createTestProposal(
	tb testing.TB,
	activeSet types.ATXIDList,
	lid types.LayerID,
	meshHash types.Hash32,
	atxID types.ATXID,
	nodeId types.NodeID,
	txIDs []types.TransactionID,
	beacon types.Beacon,
	numEligibility int,
) *types.Proposal {
	tb.Helper()
	p := &types.Proposal{
		InnerProposal: types.InnerProposal{
			Ballot: types.Ballot{
				InnerBallot: types.InnerBallot{
					Layer: lid,
					AtxID: atxID,
					EpochData: &types.EpochData{
						Beacon:           beacon,
						EligibilityCount: uint32(numEligibility),
						ActiveSetHash:    activeSet.Hash(),
					},
				},
			},
			TxIDs:    txIDs,
			MeshHash: meshHash,
		},
	}
	p.SmesherID = nodeId
	return p
}
