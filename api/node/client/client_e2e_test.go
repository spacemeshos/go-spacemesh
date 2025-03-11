package client_test

import (
	"errors"
	"net"
	"net/http"
	"testing"

	"github.com/spacemeshos/poet/shared"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
	"go.uber.org/zap/zaptest"

	"github.com/spacemeshos/go-spacemesh/activation"
	"github.com/spacemeshos/go-spacemesh/api/node/client"
	"github.com/spacemeshos/go-spacemesh/api/node/server"
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/hare3"
	"github.com/spacemeshos/go-spacemesh/p2p/pubsub"
	pubsubMocks "github.com/spacemeshos/go-spacemesh/p2p/pubsub/mocks"
	"github.com/spacemeshos/go-spacemesh/sql/statesql"
)

const retries = 3

type mocks struct {
	atxService *activation.MockAtxService
	beacons    *server.MockbeaconService
	poetDb     *server.MockpoetDB
	hare       *server.Mockhare
	weights    *server.Mockweights
	publisher  *pubsubMocks.MockPublisher
	proposals  *server.MockproposalBuilder
}

func setupE2E(t *testing.T) (*client.NodeService, *mocks) {
	log := zaptest.NewLogger(t)

	ctrl := gomock.NewController(t)
	m := &mocks{
		atxService: activation.NewMockAtxService(ctrl),
		beacons:    server.NewMockbeaconService(ctrl),
		poetDb:     server.NewMockpoetDB(ctrl),
		hare:       server.NewMockhare(ctrl),
		weights:    server.NewMockweights(ctrl),
		publisher:  pubsubMocks.NewMockPublisher(ctrl),
		proposals:  server.NewMockproposalBuilder(ctrl),
	}

	activationServiceServer := server.NewServer(
		statesql.InMemoryTest(t),
		m.atxService,
		m.beacons,
		m.publisher,
		m.poetDb,
		m.hare,
		m.weights,
		m.proposals,
		log.Named("server"))

	listener, err := net.Listen("tcp", "localhost:0")
	require.NoError(t, err)

	syncer := server.NewMocksyncer(ctrl)
	syncer.EXPECT().IsSynced(gomock.Any()).Return(true).AnyTimes()
	server := &http.Server{
		Handler: activationServiceServer.IntoHandler(http.NewServeMux(), syncer),
	}

	go server.Serve(listener)
	t.Cleanup(func() {
		server.Close()
	})

	cfg := &client.Config{
		RetryMax: retries,
	}
	svc, err := client.NewNodeServiceClient("http://"+listener.Addr().String(), log.Named("server"), cfg)
	require.NoError(t, err)
	return svc, m
}

func Test_ActivationService_Atx(t *testing.T) {
	svc, mock := setupE2E(t)

	atxid := types.ATXID{1, 2, 3, 4}

	t.Run("not found", func(t *testing.T) {
		mock.atxService.EXPECT().Atx(gomock.Any(), atxid).Return(nil, activation.ErrNotFound)
		_, err := svc.Atx(t.Context(), atxid)
		require.ErrorIs(t, err, activation.ErrNotFound)
	})

	t.Run("found", func(t *testing.T) {
		atx := &types.ActivationTx{}
		atx.SetID(atxid)
		mock.atxService.EXPECT().Atx(gomock.Any(), atxid).Return(atx, nil)
		gotAtx, err := svc.Atx(t.Context(), atxid)
		require.NoError(t, err)
		require.Equal(t, atx, gotAtx)
	})

	t.Run("backend errors", func(t *testing.T) {
		mock.atxService.EXPECT().
			Atx(gomock.Any(), atxid).
			Times(retries+1).
			Return(nil, errors.New("ops"))
		_, err := svc.Atx(t.Context(), atxid)
		require.Error(t, err)
	})
}

func Test_ActivationService_PositioningATX(t *testing.T) {
	svc, mock := setupE2E(t)

	t.Run("found", func(t *testing.T) {
		posAtx := types.RandomATXID()
		mock.atxService.EXPECT().PositioningATX(gomock.Any(), types.EpochID(77)).Return(posAtx, nil)
		gotAtx, err := svc.PositioningATX(t.Context(), 77)
		require.NoError(t, err)
		require.Equal(t, posAtx, gotAtx)
	})

	t.Run("backend errors", func(t *testing.T) {
		mock.atxService.EXPECT().
			PositioningATX(gomock.Any(), types.EpochID(77)).
			Times(retries+1).
			Return(types.EmptyATXID, errors.New("ops"))
		_, err := svc.PositioningATX(t.Context(), 77)
		require.Error(t, err)
	})
}

func Test_ActivationService_LastATX(t *testing.T) {
	svc, mock := setupE2E(t)

	atxid := types.ATXID{1, 2, 3, 4}
	nodeid := types.NodeID{5, 6, 7, 8}

	t.Run("not found", func(t *testing.T) {
		mock.atxService.EXPECT().LastATX(gomock.Any(), nodeid).Return(nil, activation.ErrNotFound)
		_, err := svc.LastATX(t.Context(), nodeid)
		require.ErrorIs(t, err, activation.ErrNotFound)
	})

	t.Run("found", func(t *testing.T) {
		atx := &types.ActivationTx{}
		atx.SetID(atxid)
		mock.atxService.EXPECT().LastATX(gomock.Any(), nodeid).Return(atx, nil)
		gotAtx, err := svc.LastATX(t.Context(), nodeid)
		require.NoError(t, err)
		require.Equal(t, atx, gotAtx)
	})

	t.Run("backend errors", func(t *testing.T) {
		mock.atxService.EXPECT().
			LastATX(gomock.Any(), nodeid).
			Times(retries+1).
			Return(nil, errors.New("ops"))
		_, err := svc.LastATX(t.Context(), nodeid)
		require.Error(t, err)
	})
}

func Test_PublishingATX(t *testing.T) {
	blob := types.RandomBytes(50)
	t.Run("publish ATX with poet", func(t *testing.T) {
		poetProof := types.PoetProofMessage{
			PoetProof: types.PoetProof{
				MerkleProof: shared.MerkleProof{
					Root: types.RandomBytes(32),
					ProvenLeaves: [][]byte{
						types.RandomBytes(32),
						types.RandomBytes(32),
					},
					ProofNodes: [][]byte{
						types.RandomBytes(32),
						types.RandomBytes(32),
					},
				},
				LeafCount: 1236,
			},
			Statement:     types.RandomHash(),
			PoetServiceID: types.RandomBytes(32),
			RoundID:       "1",
		}
		svc, mocks := setupE2E(t)
		mocks.publisher.EXPECT().Publish(gomock.Any(), pubsub.AtxProtocol, blob)
		mocks.poetDb.EXPECT().ValidateAndStore(gomock.Any(), &poetProof)
		svc.PublishATX(t.Context(), blob, &poetProof)
	})
	t.Run("publish only ATX (poet proof is optional)", func(t *testing.T) {
		svc, mocks := setupE2E(t)
		mocks.publisher.EXPECT().Publish(gomock.Any(), pubsub.AtxProtocol, blob)
		svc.PublishATX(t.Context(), blob, nil)
	})
}

func Test_Beacon(t *testing.T) {
	svc, mock := setupE2E(t)
	t.Run("beacon", func(t *testing.T) {
		beacon := types.Beacon{12, 12, 12, 12}
		mock.beacons.EXPECT().Beacon(gomock.Any(), types.EpochID(15)).Return(beacon, nil)
		v, err := svc.Beacon(t.Context(), types.EpochID(15))
		require.NoError(t, err)
		require.Equal(t, v, beacon)
	})
}

func Test_Hare(t *testing.T) {
	svc, mock := setupE2E(t)
	t.Run("total weight", func(t *testing.T) {
		val := uint64(11)
		mock.weights.EXPECT().TotalWeight(gomock.Any(), types.EpochID(112)).Return(val, nil)
		v, err := svc.TotalWeight(t.Context(), 112)
		require.NoError(t, err)
		require.Equal(t, v, val)
	})
	t.Run("miner weight", func(t *testing.T) {
		val := uint64(101)
		nodeID := types.RandomNodeID()
		mock.weights.EXPECT().MinerWeight(gomock.Any(), types.EpochID(113), nodeID).Return(val, nil)
		v, err := svc.MinerWeight(t.Context(), 113, nodeID)
		require.NoError(t, err)
		require.Equal(t, v, val)
	})
	t.Run("hare message", func(t *testing.T) {
		body := hare3.Body{
			Layer:     113,
			IterRound: hare3.IterRound{Iter: 7, Round: 2},
			Value: hare3.Value{
				Proposals: []types.ProposalID{
					types.RandomProposalID(),
					types.RandomProposalID(),
				},
			},
		}
		mock.hare.EXPECT().RoundTemplate(gomock.Any(), gomock.Any()).Return(&body)
		gotBody, err := svc.HareRoundTemplate(t.Context(), body.Layer, body.IterRound)
		require.NoError(t, err)
		require.Equal(t, body, *gotBody)

		// non-nil reference
		ref := types.RandomHash()
		body.Value.Reference = &ref
		mock.hare.EXPECT().RoundTemplate(gomock.Any(), gomock.Any()).Return(&body)
		gotBody, err = svc.HareRoundTemplate(t.Context(), body.Layer, body.IterRound)
		require.NoError(t, err)
		require.Equal(t, body, *gotBody)

		// no template
		mock.hare.EXPECT().RoundTemplate(gomock.Any(), gomock.Any()).Return(nil)
		gotBody, err = svc.HareRoundTemplate(t.Context(), body.Layer, body.IterRound)
		require.NoError(t, err)
		require.Nil(t, gotBody)
	})
}

func TestProposals(t *testing.T) {
	t.Run("build for", func(t *testing.T) {
		svc, mock := setupE2E(t)
		p := createProposal(t, true)
		mock.proposals.EXPECT().BuildFor(gomock.Any(), gomock.Any(), gomock.Any()).Return(p, 0, nil)
		prop, _, err := svc.Proposal(t.Context(), p.Layer, p.SmesherID)
		require.NoError(t, err)
		prop.MustInitialize()
		require.EqualValues(t, p, prop)
	})
	t.Run("build for - no eligibility", func(t *testing.T) {
		svc, mock := setupE2E(t)
		p := createProposal(t, false)
		mock.proposals.EXPECT().BuildFor(gomock.Any(), gomock.Any(), gomock.Any()).Return(p, 0, nil)
		prop, _, err := svc.Proposal(t.Context(), types.LayerID(112), types.NodeID{})
		require.NoError(t, err)
		require.Empty(t, prop)
	})
}

func TestCalculateEligibilitySlots(t *testing.T) {
	t.Run("smesher has eligibility", func(t *testing.T) {
		svc, mock := setupE2E(t)
		node := types.RandomNodeID()
		epoch := types.EpochID(5)
		mock.proposals.EXPECT().CalculateEligibilitySlotsFor(gomock.Any(), node, epoch).Return(10, 11111, nil)
		slots, vrfNonce, err := svc.CalculateEligibilitySlotsFor(t.Context(), node, epoch)
		require.NoError(t, err)
		require.EqualValues(t, 10, slots)
		require.EqualValues(t, 11111, vrfNonce)
	})
	t.Run("smesher doesn't have an ATX - 0 slots returned", func(t *testing.T) {
		svc, mock := setupE2E(t)
		node := types.RandomNodeID()
		epoch := types.EpochID(5)
		mock.proposals.EXPECT().CalculateEligibilitySlotsFor(gomock.Any(), node, epoch).Return(0, 0, nil)
		slots, _, err := svc.CalculateEligibilitySlotsFor(t.Context(), node, epoch)
		require.NoError(t, err)
		require.Zero(t, slots)
	})
}

func createProposal(tb testing.TB, eligible bool) *types.Proposal {
	tb.Helper()
	b := types.RandomBallot()
	b.Layer = 10000
	b.EligibilityProofs = nil
	p := &types.Proposal{
		InnerProposal: types.InnerProposal{
			Ballot: *b,
			TxIDs:  []types.TransactionID{types.RandomTransactionID(), types.RandomTransactionID()},
		},
	}
	p.Ballot.EpochData = &types.EpochData{
		EligibilityCount: 1,
	}
	p.Ballot.RefBallot = types.EmptyBallotID
	if !eligible {
		p.Ballot.EpochData.EligibilityCount = 0
	}

	nodeID := types.RandomNodeID()
	p.Ballot.SmesherID = nodeID
	p.SmesherID = nodeID
	require.NoError(tb, p.Initialize())
	return p
}
