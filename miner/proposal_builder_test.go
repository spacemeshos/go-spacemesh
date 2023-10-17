package miner

import (
	"bytes"
	"context"
	"errors"
	"math/rand"
	"os"
	"sort"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
	"go.uber.org/zap/zapcore"
	"golang.org/x/sync/errgroup"

	"github.com/spacemeshos/go-spacemesh/codec"
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/datastore"
	"github.com/spacemeshos/go-spacemesh/log/logtest"
	"github.com/spacemeshos/go-spacemesh/miner/mocks"
	"github.com/spacemeshos/go-spacemesh/p2p/pubsub"
	pmocks "github.com/spacemeshos/go-spacemesh/p2p/pubsub/mocks"
	"github.com/spacemeshos/go-spacemesh/proposals"
	"github.com/spacemeshos/go-spacemesh/signing"
	"github.com/spacemeshos/go-spacemesh/sql"
	"github.com/spacemeshos/go-spacemesh/sql/activesets"
	"github.com/spacemeshos/go-spacemesh/sql/atxs"
	"github.com/spacemeshos/go-spacemesh/sql/ballots"
	"github.com/spacemeshos/go-spacemesh/sql/beacons"
	"github.com/spacemeshos/go-spacemesh/sql/blocks"
	"github.com/spacemeshos/go-spacemesh/sql/certificates"
	"github.com/spacemeshos/go-spacemesh/sql/identities"
	"github.com/spacemeshos/go-spacemesh/sql/layers"
	smocks "github.com/spacemeshos/go-spacemesh/system/mocks"
)

const layersPerEpoch = 5

func TestMain(m *testing.M) {
	types.SetLayersPerEpoch(layersPerEpoch)

	res := m.Run()
	os.Exit(res)
}

type genAtxOpt func(*types.ActivationTx)

func genAtxWithNonce(nonce types.VRFPostIndex) genAtxOpt {
	return func(atx *types.ActivationTx) {
		atx.VRFNonce = &nonce
	}
}

func genAtxWithReceived(received time.Time) genAtxOpt {
	return func(atx *types.ActivationTx) {
		atx.SetReceived(received)
	}
}

func gatx(
	id types.ATXID,
	epoch types.EpochID,
	smesher types.NodeID,
	units uint32,
	opts ...genAtxOpt,
) *types.VerifiedActivationTx {
	atx := &types.ActivationTx{}
	atx.NumUnits = units
	atx.PublishEpoch = epoch
	atx.SmesherID = smesher
	atx.SetID(id)
	atx.SetEffectiveNumUnits(atx.NumUnits)
	atx.SetReceived(time.Time{}.Add(1))
	for _, opt := range opts {
		opt(atx)
	}
	verified, err := atx.Verify(0, 100)
	if err != nil {
		panic(err)
	}
	return verified
}

func gactiveset(atxs ...types.ATXID) types.ATXIDList {
	return atxs
}

func gballot(
	id types.BallotID,
	atxid types.ATXID,
	smesher types.NodeID,
	layer types.LayerID,
	edata *types.EpochData,
) *types.Ballot {
	ballot := &types.Ballot{}
	ballot.Layer = layer
	ballot.EpochData = edata
	ballot.AtxID = atxid
	ballot.SmesherID = smesher
	ballot.SetID(id)
	return ballot
}

func gblock(lid types.LayerID, atxs ...types.ATXID) *types.Block {
	block := types.Block{}
	block.LayerIndex = lid
	for _, atx := range atxs {
		block.Rewards = append(block.Rewards, types.AnyReward{AtxID: atx})
	}
	block.Initialize()
	return &block
}

type expectOpt func(p *types.Proposal)

func expectEpochData(set types.ATXIDList, slots uint32, beacon types.Beacon) expectOpt {
	return func(p *types.Proposal) {
		p.EpochData = &types.EpochData{
			ActiveSetHash:    set.Hash(),
			EligibilityCount: slots,
			Beacon:           beacon,
		}
	}
}

func expectRef(id types.BallotID) expectOpt {
	return func(p *types.Proposal) {
		p.RefBallot = id
	}
}

func expectTxs(txs []types.TransactionID) expectOpt {
	return func(p *types.Proposal) {
		p.TxIDs = txs
	}
}

func expectMeshHash(hash types.Hash32) expectOpt {
	return func(p *types.Proposal) {
		p.MeshHash = hash
	}
}

func expectCounters(
	signer *signing.EdSigner,
	epoch types.EpochID,
	beacon types.Beacon,
	nonce types.VRFPostIndex,
	js ...uint32,
) expectOpt {
	return func(p *types.Proposal) {
		vsigner := signer.VRFSigner()
		for _, j := range js {
			p.EligibilityProofs = append(p.EligibilityProofs, types.VotingEligibility{
				J:   j,
				Sig: vsigner.Sign(proposals.MustSerializeVRFMessage(beacon, epoch, nonce, j)),
			})
		}
	}
}

func expectProposal(
	signer *signing.EdSigner,
	lid types.LayerID,
	atx types.ATXID,
	opinion types.Opinion,
	opts ...expectOpt,
) *types.Proposal {
	p := &types.Proposal{
		InnerProposal: types.InnerProposal{
			Ballot: types.Ballot{
				InnerBallot: types.InnerBallot{
					Layer:       lid,
					AtxID:       atx,
					OpinionHash: opinion.Hash,
				},
				Votes: opinion.Votes,
			},
		},
	}
	for _, opt := range opts {
		opt(p)
	}
	p.Ballot.Signature = signer.Sign(signing.BALLOT, p.Ballot.SignedBytes())
	p.SmesherID = signer.NodeID()
	p.Signature = signer.Sign(signing.PROPOSAL, p.SignedBytes())
	if err := p.Initialize(); err != nil {
		panic(err)
	}
	return p
}

type identity struct {
	id       types.NodeID
	proof    types.MalfeasanceProof
	received time.Time
}

type aggHash struct {
	lid  types.LayerID
	hash types.Hash32
}

type step struct {
	lid          types.LayerID
	beacon       types.Beacon
	atxs         []*types.VerifiedActivationTx
	ballots      []*types.Ballot
	activeset    types.ATXIDList
	identitities []identity
	blocks       []*types.Block
	hare         []types.LayerID
	aggHashes    []aggHash

	txs            []types.TransactionID
	latestComplete types.LayerID
	opinion        *types.Opinion

	encodeVotesErr, publishErr error

	expectProposal  *types.Proposal
	expectProposals []*types.Proposal
	expectErr       string
}

func TestBuild(t *testing.T) {
	signers := make([]*signing.EdSigner, 4)
	rng := rand.New(rand.NewSource(10101))
	for i := range signers {
		signer, err := signing.NewEdSigner(signing.WithKeyFromRand(rng))
		require.NoError(t, err)
		signers[i] = signer
	}
	signer := signers[0]
	defaults := []Opt{
		WithLayerPerEpoch(types.GetLayersPerEpoch()),
		WithLayerSize(10),
	}
	for _, tc := range []struct {
		desc  string
		opts  []Opt
		steps []step
	}{
		{
			desc: "sanity reference",
			steps: []step{
				{
					lid:    15,
					beacon: types.Beacon{1},
					atxs: []*types.VerifiedActivationTx{
						gatx(types.ATXID{1}, 2, signer.NodeID(), 1, genAtxWithNonce(777)),
						gatx(types.ATXID{2}, 2, types.NodeID{2}, 1),
						gatx(types.ATXID{3}, 2, types.NodeID{3}, 1),
						gatx(types.ATXID{4}, 2, types.NodeID{4}, 1),
					},
					opinion:        &types.Opinion{Hash: types.Hash32{1}},
					txs:            []types.TransactionID{{1}, {2}},
					latestComplete: 14,
					expectProposal: expectProposal(
						signer, 15, types.ATXID{1}, types.Opinion{Hash: types.Hash32{1}},
						expectEpochData(
							gactiveset(types.ATXID{1}, types.ATXID{2}, types.ATXID{3}, types.ATXID{4}),
							12,
							types.Beacon{1},
						),
						expectTxs([]types.TransactionID{{1}, {2}}),
						expectCounters(signer, 3, types.Beacon{1}, 777, 0, 6, 9),
					),
				},
			},
		},
		{
			desc: "min active weight",
			opts: []Opt{WithMinimalActiveSetWeight(1000)},
			steps: []step{
				{
					lid:    15,
					beacon: types.Beacon{1},
					atxs: []*types.VerifiedActivationTx{
						gatx(types.ATXID{1}, 2, signer.NodeID(), 1, genAtxWithNonce(777)),
					},
					opinion:        &types.Opinion{Hash: types.Hash32{1}},
					txs:            []types.TransactionID{},
					latestComplete: 14,
					expectProposal: expectProposal(
						signer, 15, types.ATXID{1}, types.Opinion{Hash: types.Hash32{1}},
						expectEpochData(
							gactiveset(types.ATXID{1}),
							5,
							types.Beacon{1},
						),
						expectCounters(signer, 3, types.Beacon{1}, 777, 0),
					),
				},
			},
		},
		{
			desc: "sanity secondary",
			steps: []step{
				{
					lid:    16,
					beacon: types.Beacon{1},
					atxs: []*types.VerifiedActivationTx{
						gatx(types.ATXID{1}, 2, signer.NodeID(), 1, genAtxWithNonce(777)),
						gatx(types.ATXID{2}, 2, types.NodeID{2}, 1),
					},
					ballots: []*types.Ballot{
						gballot(types.BallotID{1}, types.ATXID{1}, signer.NodeID(), 15, &types.EpochData{
							ActiveSetHash:    types.ATXIDList{{1}, {2}}.Hash(),
							EligibilityCount: 5,
							Beacon:           types.Beacon{1},
						}),
					},
					activeset:      types.ATXIDList{{1}, {2}},
					txs:            []types.TransactionID{},
					opinion:        &types.Opinion{Hash: types.Hash32{1}},
					latestComplete: 15,
					expectProposal: expectProposal(
						signer, 16, types.ATXID{1}, types.Opinion{Hash: types.Hash32{1}},
						expectRef(types.BallotID{1}),
						expectCounters(signer, 3, types.Beacon{1}, 777, 2),
					),
				},
			},
		},
		{
			desc: "no data",
			steps: []step{
				{
					lid:       15,
					expectErr: "missing beacon",
				},
				{
					lid:       15,
					beacon:    types.Beacon{1},
					expectErr: "empty active set",
				},
				{
					lid: 15,
					atxs: []*types.VerifiedActivationTx{
						gatx(types.ATXID{20}, 2, types.NodeID{20}, 1),
					},
				},
				{
					lid: 15,
					atxs: []*types.VerifiedActivationTx{
						gatx(types.ATXID{10}, 2, signer.NodeID(), 1),
					},
					expectErr: "missing nonce",
				},
				{
					lid: 16,
					atxs: []*types.VerifiedActivationTx{
						gatx(types.ATXID{1}, 2, signer.NodeID(), 1, genAtxWithNonce(777)),
					},
					ballots: []*types.Ballot{
						gballot(types.BallotID{1}, types.ATXID{10}, signer.NodeID(), 15, &types.EpochData{
							ActiveSetHash:    types.ATXIDList{{10}, {2}}.Hash(),
							EligibilityCount: 5,
							Beacon:           types.Beacon{1},
						}),
					},
					opinion:        &types.Opinion{Hash: types.Hash32{1}},
					txs:            []types.TransactionID{{1}},
					latestComplete: 14,
					expectProposal: expectProposal(
						signer, 16, types.ATXID{10}, types.Opinion{Hash: types.Hash32{1}},
						expectRef(types.BallotID{1}),
						expectTxs([]types.TransactionID{{1}}),
						expectCounters(signer, 3, types.Beacon{1}, 777, 2),
					),
				},
			},
		},
		{
			desc: "not eligible",
			steps: []step{
				{
					lid:    16,
					beacon: types.Beacon{1},
					atxs: []*types.VerifiedActivationTx{
						gatx(types.ATXID{1}, 2, signer.NodeID(), 1, genAtxWithNonce(777)),
						gatx(types.ATXID{2}, 2, types.NodeID{2}, 100),
					},
					ballots: []*types.Ballot{
						gballot(types.BallotID{1}, types.ATXID{1}, signer.NodeID(), 15, &types.EpochData{
							ActiveSetHash:    types.ATXIDList{{1}, {2}}.Hash(),
							EligibilityCount: 1,
						}),
					},
					activeset: types.ATXIDList{{1}, {2}},
				},
				{
					lid:       16,
					expectErr: "was already built",
				},
			},
		},
		{
			desc: "encode votes error",
			steps: []step{
				{
					lid:    16,
					beacon: types.Beacon{1},
					atxs: []*types.VerifiedActivationTx{
						gatx(types.ATXID{1}, 2, signer.NodeID(), 1, genAtxWithNonce(777)),
					},
					ballots: []*types.Ballot{
						gballot(types.BallotID{1}, types.ATXID{1}, signer.NodeID(), 15, &types.EpochData{
							ActiveSetHash:    types.ATXIDList{{1}}.Hash(),
							EligibilityCount: 10,
							Beacon:           types.Beacon{1},
						}),
					},
					activeset:      types.ATXIDList{{1}},
					opinion:        &types.Opinion{},
					encodeVotesErr: errors.New("test votes"),
					expectErr:      "test votes",
				},
			},
		},
		{
			desc: "publish error",
			steps: []step{
				{
					lid:    16,
					beacon: types.Beacon{1},
					atxs: []*types.VerifiedActivationTx{
						gatx(types.ATXID{1}, 2, signer.NodeID(), 1, genAtxWithNonce(777)),
					},
					ballots: []*types.Ballot{
						gballot(types.BallotID{1}, types.ATXID{1}, signer.NodeID(), 15, &types.EpochData{
							ActiveSetHash:    types.ATXIDList{{1}}.Hash(),
							EligibilityCount: 10,
							Beacon:           types.Beacon{1},
						}),
					},
					activeset:      types.ATXIDList{{1}},
					latestComplete: 15,
					opinion:        &types.Opinion{},
					txs:            []types.TransactionID{},
					publishErr:     errors.New("test publish"),
					expectProposal: expectProposal(
						signer, 16, types.ATXID{1}, types.Opinion{},
						expectRef(types.BallotID{1}),
						expectCounters(signer, 3, types.Beacon{1}, 777, 2, 5),
					),
				},
			},
		},
		{
			desc: "malicious is not added to activeset",
			steps: []step{
				{
					lid:    16,
					beacon: types.Beacon{1},
					atxs: []*types.VerifiedActivationTx{
						gatx(types.ATXID{1}, 2, signer.NodeID(), 1, genAtxWithNonce(777)),
						gatx(types.ATXID{2}, 2, types.NodeID{2}, 1),
					},
					identitities: []identity{{
						id: types.NodeID{2},
						proof: types.MalfeasanceProof{Proof: types.Proof{
							Type: types.HareEquivocation,
							Data: &types.HareProof{},
						}},
					}},
					opinion:        &types.Opinion{Hash: types.Hash32{1}},
					txs:            []types.TransactionID{},
					latestComplete: 15,
					expectProposal: expectProposal(
						signer, 16, types.ATXID{1}, types.Opinion{Hash: types.Hash32{1}},
						expectEpochData(
							gactiveset(types.ATXID{1}),
							50,
							types.Beacon{1},
						),
						expectCounters(signer, 3, types.Beacon{1}, 777,
							2, 5, 11, 19, 22, 24, 28, 30, 33, 36),
					),
				},
			},
		},
		{
			desc: "first block activeset",
			opts: []Opt{
				WithNetworkDelay(10 * time.Second),
				WithMinGoodAtxPercent(50),
			},
			steps: []step{
				{
					lid:    16,
					beacon: types.Beacon{1},
					atxs: []*types.VerifiedActivationTx{
						gatx(types.ATXID{1}, 2, signer.NodeID(), 1, genAtxWithNonce(777)),
						gatx(types.ATXID{2}, 2, types.NodeID{2}, 1, genAtxWithReceived(time.Unix(20, 0))),
						gatx(types.ATXID{3}, 2, types.NodeID{3}, 1, genAtxWithReceived(time.Unix(20, 0))),
					},
					expectErr: "first block",
				},
				{
					lid: 16,
					blocks: []*types.Block{
						gblock(15, types.ATXID{4}), // this atx and ballot doesn't exist
					},
					expectErr: "actives get ballot",
				},
				{
					lid: 16,
					ballots: []*types.Ballot{
						gballot(types.BallotID{11}, types.ATXID{4}, types.NodeID{5}, 15, &types.EpochData{
							ActiveSetHash: gactiveset(types.ATXID{1}, types.ATXID{2}).Hash(),
						}),
					},
					expectErr: "get active hash for ballot",
				},
				{
					lid:       16,
					activeset: gactiveset(types.ATXID{1}, types.ATXID{2}),
					expectErr: "get ATXs from DB: get id 0400000000",
				},
				{
					lid: 16,
					atxs: []*types.VerifiedActivationTx{
						gatx(types.ATXID{4}, 2, types.NodeID{4}, 1, genAtxWithReceived(time.Unix(20, 0))),
					},
					opinion:        &types.Opinion{Hash: types.Hash32{1}},
					txs:            []types.TransactionID{},
					latestComplete: 15,
					expectProposal: expectProposal(
						signer, 16, types.ATXID{1}, types.Opinion{Hash: types.Hash32{1}},
						expectEpochData(
							gactiveset(types.ATXID{1}, types.ATXID{2}, types.ATXID{4}),
							16,
							types.Beacon{1},
						),
						expectCounters(signer, 3, types.Beacon{1}, 777, 2, 5, 11),
					),
				},
			},
		},
		{
			desc: "invalid first ballot",
			steps: []step{
				{
					lid:    16,
					beacon: types.Beacon{1},
					atxs: []*types.VerifiedActivationTx{
						gatx(types.ATXID{1}, 2, signer.NodeID(), 1, genAtxWithNonce(777)),
					},
					ballots: []*types.Ballot{
						gballot(types.BallotID{11}, types.ATXID{1}, signer.NodeID(), 15, nil),
					},
					expectErr: "first ballot",
				},
			},
		},
		{
			desc: "mesh hash selection",
			opts: []Opt{WithHdist(2)},
			steps: []step{
				{
					lid:    16,
					beacon: types.Beacon{1},
					atxs: []*types.VerifiedActivationTx{
						gatx(types.ATXID{1}, 2, signer.NodeID(), 1, genAtxWithNonce(777)),
					},
					ballots: []*types.Ballot{
						gballot(types.BallotID{1}, types.ATXID{1}, signer.NodeID(), 15, &types.EpochData{
							ActiveSetHash:    types.ATXIDList{{1}}.Hash(),
							EligibilityCount: 10,
							Beacon:           types.Beacon{1},
						}),
					},
					activeset:      types.ATXIDList{{1}},
					txs:            []types.TransactionID{},
					opinion:        &types.Opinion{Hash: types.Hash32{1}},
					latestComplete: 5, // layers outside hdist not verified
					expectProposal: expectProposal(
						signer, 16, types.ATXID{1}, types.Opinion{Hash: types.Hash32{1}},
						expectRef(types.BallotID{1}),
						expectCounters(signer, 3, types.Beacon{1}, 777, 2, 5),
					),
				},
				{
					lid:            17,
					txs:            []types.TransactionID{},
					opinion:        &types.Opinion{Hash: types.Hash32{1}},
					latestComplete: 15, // missing hare output for layer within hdist
					expectProposal: expectProposal(
						signer, 17, types.ATXID{1}, types.Opinion{Hash: types.Hash32{1}},
						expectRef(types.BallotID{1}),
						expectCounters(signer, 3, types.Beacon{1}, 777, 3, 7),
					),
				},
				{
					lid:            18,
					txs:            []types.TransactionID{},
					opinion:        &types.Opinion{Hash: types.Hash32{1}},
					latestComplete: 15,
					hare:           []types.LayerID{16, 17}, // failed to get mesh hash
					expectProposal: expectProposal(
						signer, 18, types.ATXID{1}, types.Opinion{Hash: types.Hash32{1}},
						expectRef(types.BallotID{1}),
						expectCounters(signer, 3, types.Beacon{1}, 777, 1, 4, 8),
					),
				},
			},
		},
		{
			desc: "mesh hash selection",
			opts: []Opt{WithHdist(10)},
			steps: []step{
				{
					lid:    16,
					beacon: types.Beacon{1},
					atxs: []*types.VerifiedActivationTx{
						gatx(types.ATXID{1}, 2, signer.NodeID(), 1, genAtxWithNonce(777)),
					},
					ballots: []*types.Ballot{
						gballot(types.BallotID{1}, types.ATXID{1}, signer.NodeID(), 15, &types.EpochData{
							ActiveSetHash:    types.ATXIDList{{1}}.Hash(),
							EligibilityCount: 10,
							Beacon:           types.Beacon{1},
						}),
					},
					activeset:      types.ATXIDList{{1}},
					txs:            []types.TransactionID{},
					opinion:        &types.Opinion{Hash: types.Hash32{1}},
					latestComplete: 8,
					expectProposal: expectProposal(
						signer, 16, types.ATXID{1}, types.Opinion{Hash: types.Hash32{1}},
						expectRef(types.BallotID{1}),
						expectCounters(signer, 3, types.Beacon{1}, 777, 2, 5),
					),
				},
				{
					lid:            17,
					txs:            []types.TransactionID{},
					opinion:        &types.Opinion{Hash: types.Hash32{1}},
					latestComplete: 15,
					hare:           []types.LayerID{9, 10, 11, 12, 13, 14, 15, 16},
					aggHashes:      []aggHash{{lid: 16, hash: types.Hash32{9, 9, 9}}},
					expectProposal: expectProposal(
						signer, 17, types.ATXID{1}, types.Opinion{Hash: types.Hash32{1}},
						expectRef(types.BallotID{1}),
						expectMeshHash(types.Hash32{9, 9, 9}),
						expectCounters(signer, 3, types.Beacon{1}, 777, 3, 7),
					),
				},
			},
		},
		{
			desc: "multi signers",
			opts: []Opt{WithSigners(signers...), WithWorkersLimit(len(signers) / 2)},
			steps: []step{
				{
					lid:    15,
					beacon: types.Beacon{1},
					atxs: []*types.VerifiedActivationTx{
						gatx(types.ATXID{1}, 2, signers[0].NodeID(), 1, genAtxWithNonce(777)),
						gatx(types.ATXID{2}, 2, signers[1].NodeID(), 1, genAtxWithNonce(999)),
					},
					opinion:        &types.Opinion{Hash: types.Hash32{1}},
					txs:            []types.TransactionID{{1}, {2}},
					latestComplete: 14,
					expectProposals: []*types.Proposal{
						expectProposal(
							signers[0], 15, types.ATXID{1}, types.Opinion{Hash: types.Hash32{1}},
							expectEpochData(
								gactiveset(types.ATXID{1}, types.ATXID{2}),
								25,
								types.Beacon{1},
							),
							expectTxs([]types.TransactionID{{1}, {2}}),
							expectCounters(signers[0], 3, types.Beacon{1}, 777, 0, 6, 9, 12, 16, 18, 20, 23),
						),
						expectProposal(
							signers[1], 15, types.ATXID{2}, types.Opinion{Hash: types.Hash32{1}},
							expectEpochData(
								gactiveset(types.ATXID{1}, types.ATXID{2}),
								25,
								types.Beacon{1},
							),
							expectTxs([]types.TransactionID{{1}, {2}}),
							expectCounters(signers[1], 3, types.Beacon{1}, 999, 0, 4, 6, 8, 9, 17),
						),
					},
				},
			},
		},
	} {
		tc := tc
		t.Run(tc.desc, func(t *testing.T) {
			var (
				ctx       = context.Background()
				ctrl      = gomock.NewController(t)
				conState  = mocks.NewMockconservativeState(ctrl)
				clock     = mocks.NewMocklayerClock(ctrl)
				publisher = pmocks.NewMockPublisher(ctrl)
				tortoise  = mocks.NewMockvotesEncoder(ctrl)
				syncer    = smocks.NewMockSyncStateProvider(ctrl)
				cdb       = datastore.NewCachedDB(sql.InMemory(), logtest.New(t))
			)

			clock.EXPECT().LayerToTime(gomock.Any()).Return(time.Unix(0, 0)).AnyTimes()

			full := append(defaults, WithLogger(logtest.New(t)), WithSigners(signer))
			full = append(full, tc.opts...)
			builder := New(clock, cdb, publisher, tortoise, syncer, conState, full...)
			var decoded chan *types.Proposal
			for _, step := range tc.steps {
				{
					if step.beacon != types.EmptyBeacon {
						require.NoError(t, beacons.Add(cdb, step.lid.GetEpoch(), step.beacon))
					}
					for _, iden := range step.identitities {
						require.NoError(
							t,
							identities.SetMalicious(
								cdb,
								iden.id,
								codec.MustEncode(&iden.proof),
								iden.received,
							),
						)
					}
					for _, atx := range step.atxs {
						require.NoError(t, atxs.Add(cdb, atx))
					}
					for _, ballot := range step.ballots {
						require.NoError(t, ballots.Add(cdb, ballot))
					}
					for _, block := range step.blocks {
						require.NoError(t, blocks.Add(cdb, block))
						require.NoError(t, layers.SetApplied(cdb, block.LayerIndex, block.ID()))
					}
					for _, lid := range step.hare {
						// block id is irrelevant for this test
						require.NoError(t, certificates.SetHareOutput(cdb, lid, types.EmptyBlockID))
					}
					for _, ahash := range step.aggHashes {
						require.NoError(t, layers.SetMeshHash(cdb, ahash.lid, ahash.hash))
					}
					if step.activeset != nil {
						require.NoError(
							t,
							activesets.Add(
								cdb,
								step.activeset.Hash(),
								&types.EpochActiveSet{Set: step.activeset},
							),
						)
					}
				}
				{
					if step.opinion != nil {
						tortoise.EXPECT().TallyVotes(ctx, step.lid)
						tortoise.EXPECT().
							EncodeVotes(ctx, gomock.Any()).
							Return(step.opinion, step.encodeVotesErr)
					}
					if step.txs != nil {
						conState.EXPECT().
							SelectProposalTXs(step.lid, gomock.Any()).
							Return(step.txs).
							AnyTimes()
					}
					if step.latestComplete != 0 {
						tortoise.EXPECT().LatestComplete().Return(step.latestComplete)
					}
				}
				decoded = make(
					chan *types.Proposal,
					len(signers),
				) // set the maximum possible capacity
				if step.expectProposal != nil || len(step.expectProposals) > 0 ||
					step.publishErr != nil {
					publisher.EXPECT().
						Publish(ctx, pubsub.ProposalProtocol, gomock.Any()).
						DoAndReturn(func(_ context.Context, _ string, msg []byte) error {
							var proposal types.Proposal
							codec.MustDecode(msg, &proposal)
							proposal.MustInitialize()
							select {
							case decoded <- &proposal:
							default:
								require.FailNow(
									t,
									"blocking in Publish. check decoded channel capacity",
								)
							}
							return step.publishErr
						}).
						AnyTimes()
				}
				err := builder.build(ctx, step.lid)
				close(decoded)
				if len(step.expectErr) > 0 {
					require.ErrorContains(t, err, step.expectErr)
				} else {
					require.NoError(t, err)
					expect := step.expectProposals
					if step.expectProposal != nil {
						expect = []*types.Proposal{step.expectProposal}
					}
					received := []*types.Proposal{}
					for proposal := range decoded {
						received = append(received, proposal)
					}
					sort.Slice(received, func(i, j int) bool {
						return bytes.Compare(received[i].SmesherID[:], received[j].SmesherID[:]) == -1
					})
					require.Len(t, received, len(expect))
					for i := range expect {
						require.Equal(t, expect[i], received[i], "i=%d", i)
					}
				}
			}
		})
	}
}

func TestMarshalLog(t *testing.T) {
	encoder := zapcore.NewMapObjectEncoder()
	t.Run("config", func(t *testing.T) {
		cfg := &config{}
		require.NoError(t, cfg.MarshalLogObject(encoder))
	})
	t.Run("session", func(t *testing.T) {
		session := &session{}
		session.ref = types.BallotID{1}
		session.eligibilities.proofs = map[types.LayerID][]types.VotingEligibility{
			10: {{J: 5}},
			12: {{J: 7}},
		}
		require.NoError(t, session.MarshalLogObject(encoder))
	})
	t.Run("latency", func(t *testing.T) {
		latency := &latencyTracker{start: time.Unix(0, 0), publish: time.Unix(1000, 0)}
		require.NoError(t, latency.MarshalLogObject(encoder))
	})
}

func TestStartStop(t *testing.T) {
	var (
		ctrl      = gomock.NewController(t)
		conState  = mocks.NewMockconservativeState(ctrl)
		clock     = mocks.NewMocklayerClock(ctrl)
		publisher = pmocks.NewMockPublisher(ctrl)
		tortoise  = mocks.NewMockvotesEncoder(ctrl)
		syncer    = smocks.NewMockSyncStateProvider(ctrl)
		cdb       = datastore.NewCachedDB(sql.InMemory(), logtest.New(t))
	)
	signer, err := signing.NewEdSigner(signing.WithKeyFromRand(rand.New(rand.NewSource(10101))))
	require.NoError(t, err)

	layers := [...]types.LayerID{
		1, 2, 3, 5, 10, 9, 11, 12, 13,
	}
	current := 0

	clock.EXPECT().CurrentLayer().DoAndReturn(func() types.LayerID {
		rst := layers[current]
		if current < len(layers)-1 {
			current++
		}
		return rst
	}).AnyTimes()

	wait := make(chan struct{})
	clock.EXPECT().AwaitLayer(gomock.Any()).DoAndReturn(func(lid types.LayerID) <-chan struct{} {
		closed := make(chan struct{})
		close(closed)
		if lid <= layers[len(layers)-1] {
			return closed
		}
		select {
		case <-wait:
		default:
			close(wait)
		}
		return make(chan struct{})
	}).AnyTimes()
	syncer.EXPECT().IsSynced(gomock.Any()).Return(true).AnyTimes()

	builder := New(
		clock,
		cdb,
		publisher,
		tortoise,
		syncer,
		conState,
		WithLogger(logtest.New(t)),
	)
	builder.Register(signer)
	var (
		ctx, cancel = context.WithCancel(context.Background())
		eg          errgroup.Group
	)
	eg.Go(func() error {
		return builder.Run(ctx)
	})
	t.Cleanup(func() {
		cancel()
		require.NoError(t, eg.Wait())
	})
	select {
	case <-time.After(time.Second):
		require.FailNow(t, "test didn't complete in 1s")
	case <-wait:
	}
}

func TestGradeAtx(t *testing.T) {
	const delta = 10
	for _, tc := range []struct {
		desc      string
		malicious bool
		// distance in second from the epoch start time
		atxReceived, malReceived int
		result                   atxGrade
	}{
		{
			desc:        "very early atx",
			atxReceived: -41,
			result:      good,
		},
		{
			desc:        "very early atx, late malfeasance",
			atxReceived: -41,
			malicious:   true,
			malReceived: 0,
			result:      good,
		},
		{
			desc:        "very early atx, malicious",
			atxReceived: -41,
			malicious:   true,
			malReceived: -10,
			result:      acceptable,
		},
		{
			desc:        "very early atx, early malicious",
			atxReceived: -41,
			malicious:   true,
			malReceived: -11,
			result:      evil,
		},
		{
			desc:        "early atx",
			atxReceived: -31,
			result:      acceptable,
		},
		{
			desc:        "early atx, late malicious",
			atxReceived: -31,
			malicious:   true,
			malReceived: -10,
			result:      acceptable,
		},
		{
			desc:        "early atx, early malicious",
			atxReceived: -31,
			malicious:   true,
			malReceived: -11,
			result:      evil,
		},
		{
			desc:        "late atx",
			atxReceived: -30,
			result:      evil,
		},
		{
			desc:        "very late atx",
			atxReceived: 0,
			result:      evil,
		},
	} {
		tc := tc
		t.Run(tc.desc, func(t *testing.T) {
			mockMsh := mocks.NewMockmesh(gomock.NewController(t))
			epochStart := time.Now()
			nodeID := types.RandomNodeID()
			if tc.malicious {
				proof := &types.MalfeasanceProof{}
				proof.SetReceived(epochStart.Add(time.Duration(tc.malReceived) * time.Second))
				mockMsh.EXPECT().GetMalfeasanceProof(nodeID).Return(proof, nil)
			} else {
				mockMsh.EXPECT().GetMalfeasanceProof(nodeID).Return(nil, sql.ErrNotFound)
			}
			atxReceived := epochStart.Add(time.Duration(tc.atxReceived) * time.Second)
			got, err := gradeAtx(mockMsh, nodeID, atxReceived, epochStart, delta*time.Second)
			require.NoError(t, err)
			require.Equal(t, tc.result, got)
		})
	}
}
