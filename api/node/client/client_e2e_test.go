package client_test

import (
	"context"
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
)

const retries = 3

type mocks struct {
	atxService *activation.MockAtxService
	beacons    *server.MockbeaconService
	poetDb     *server.MockpoetDB
	hare       *server.Mockhare
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
		publisher:  pubsubMocks.NewMockPublisher(ctrl),
		proposals:  server.NewMockproposalBuilder(ctrl),
	}

	activationServiceServer := server.NewServer(m.atxService,
		m.beacons,
		m.publisher,
		m.poetDb,
		m.hare,
		m.proposals,
		log.Named("server"))

	listener, err := net.Listen("tcp", "localhost:0")
	require.NoError(t, err)

	server := &http.Server{
		Handler: activationServiceServer.IntoHandler(http.NewServeMux()),
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
		_, err := svc.Atx(context.Background(), atxid)
		require.ErrorIs(t, err, activation.ErrNotFound)
	})

	t.Run("found", func(t *testing.T) {
		atx := &types.ActivationTx{}
		atx.SetID(atxid)
		mock.atxService.EXPECT().Atx(gomock.Any(), atxid).Return(atx, nil)
		gotAtx, err := svc.Atx(context.Background(), atxid)
		require.NoError(t, err)
		require.Equal(t, atx, gotAtx)
	})

	t.Run("backend errors", func(t *testing.T) {
		mock.atxService.EXPECT().
			Atx(gomock.Any(), atxid).
			Times(retries+1).
			Return(nil, errors.New("ops"))
		_, err := svc.Atx(context.Background(), atxid)
		require.Error(t, err)
	})
}

func Test_ActivationService_PositioningATX(t *testing.T) {
	svc, mock := setupE2E(t)

	t.Run("found", func(t *testing.T) {
		posAtx := types.RandomATXID()
		mock.atxService.EXPECT().PositioningATX(gomock.Any(), types.EpochID(77)).Return(posAtx, nil)
		gotAtx, err := svc.PositioningATX(context.Background(), 77)
		require.NoError(t, err)
		require.Equal(t, posAtx, gotAtx)
	})

	t.Run("backend errors", func(t *testing.T) {
		mock.atxService.EXPECT().
			PositioningATX(gomock.Any(), types.EpochID(77)).
			Times(retries+1).
			Return(types.EmptyATXID, errors.New("ops"))
		_, err := svc.PositioningATX(context.Background(), 77)
		require.Error(t, err)
	})
}

func Test_ActivationService_LastATX(t *testing.T) {
	svc, mock := setupE2E(t)

	atxid := types.ATXID{1, 2, 3, 4}
	nodeid := types.NodeID{5, 6, 7, 8}

	t.Run("not found", func(t *testing.T) {
		mock.atxService.EXPECT().LastATX(gomock.Any(), nodeid).Return(nil, activation.ErrNotFound)
		_, err := svc.LastATX(context.Background(), nodeid)
		require.ErrorIs(t, err, activation.ErrNotFound)
	})

	t.Run("found", func(t *testing.T) {
		atx := &types.ActivationTx{}
		atx.SetID(atxid)
		mock.atxService.EXPECT().LastATX(gomock.Any(), nodeid).Return(atx, nil)
		gotAtx, err := svc.LastATX(context.Background(), nodeid)
		require.NoError(t, err)
		require.Equal(t, atx, gotAtx)
	})

	t.Run("backend errors", func(t *testing.T) {
		mock.atxService.EXPECT().
			LastATX(gomock.Any(), nodeid).
			Times(retries+1).
			Return(nil, errors.New("ops"))
		_, err := svc.LastATX(context.Background(), nodeid)
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
		svc.PublishATX(context.Background(), blob, &poetProof)
	})
	t.Run("publish only ATX (poet proof is optional)", func(t *testing.T) {
		svc, mocks := setupE2E(t)
		mocks.publisher.EXPECT().Publish(gomock.Any(), pubsub.AtxProtocol, blob)
		svc.PublishATX(context.Background(), blob, nil)
	})
}

func Test_Beacon(t *testing.T) {
	svc, mock := setupE2E(t)
	t.Run("beacon", func(t *testing.T) {
		beacon := types.Beacon{12, 12, 12, 12}
		mock.beacons.EXPECT().Beacon(gomock.Any(), types.EpochID(15)).Return(beacon, nil)
		v, err := svc.Beacon(context.Background(), types.EpochID(15))
		require.NoError(t, err)
		require.Equal(t, v, beacon)
	})
}

func Test_Hare(t *testing.T) {
	svc, mock := setupE2E(t)
	t.Run("total weight", func(t *testing.T) {
		val := uint64(11)
		mock.hare.EXPECT().TotalWeight(gomock.Any(), gomock.Any()).Return(val, nil)
		v, err := svc.TotalWeight(context.Background(), 112)
		require.NoError(t, err)
		require.Equal(t, v, val)
	})
	t.Run("miner weight", func(t *testing.T) {
		val := uint64(101)
		mock.hare.EXPECT().MinerWeight(gomock.Any(), gomock.Any(), gomock.Any()).Return(val, nil)
		v, err := svc.MinerWeight(context.Background(), 113, types.NodeID{})
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
		gotBody, err := svc.HareRoundTemplate(context.Background(), body.Layer, body.IterRound)
		require.NoError(t, err)
		require.Equal(t, body, *gotBody)

		// non-nil reference
		ref := types.RandomHash()
		body.Value.Reference = &ref
		mock.hare.EXPECT().RoundTemplate(gomock.Any(), gomock.Any()).Return(&body)
		gotBody, err = svc.HareRoundTemplate(context.Background(), body.Layer, body.IterRound)
		require.NoError(t, err)
		require.Equal(t, body, *gotBody)

		// no template
		mock.hare.EXPECT().RoundTemplate(gomock.Any(), gomock.Any()).Return(nil)
		gotBody, err = svc.HareRoundTemplate(context.Background(), body.Layer, body.IterRound)
		require.NoError(t, err)
		require.Nil(t, gotBody)
	})
}

func TestProposals(t *testing.T) {
	svc, mock := setupE2E(t)
	t.Run("build for", func(t *testing.T) {
		p := createProposal(t, true)
		mock.proposals.EXPECT().BuildFor(gomock.Any(), gomock.Any(), gomock.Any()).Return(p, 0, nil)
		prop, _, err := svc.Proposal(context.Background(), p.Layer, p.SmesherID)
		require.NoError(t, err)
		prop.MustInitialize()
		require.EqualValues(t, p, prop)
	})
	svc, mock = setupE2E(t)
	t.Run("build for - no eligibility", func(t *testing.T) {
		p := createProposal(t, false)
		mock.proposals.EXPECT().BuildFor(gomock.Any(), gomock.Any(), gomock.Any()).Return(p, 0, nil)
		prop, _, err := svc.Proposal(context.Background(), types.LayerID(112), types.NodeID{})
		require.NoError(t, err)
		require.Empty(t, prop)
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
