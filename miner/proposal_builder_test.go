package miner

import (
	"context"
	"errors"
	"math/rand"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

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

func gatx(id types.ATXID, epoch types.EpochID, smesher types.NodeID, units uint32, opts ...genAtxOpt) *types.VerifiedActivationTx {
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

func gballot(id types.BallotID, atxid types.ATXID, smesher types.NodeID, layer types.LayerID, edata *types.EpochData) *types.Ballot {
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

func expectCounters(signer *signing.EdSigner, epoch types.EpochID, beacon types.Beacon, nonce types.VRFPostIndex, js ...uint32) expectOpt {
	return func(p *types.Proposal) {
		vsigner, err := signer.VRFSigner()
		if err != nil {
			panic(err)
		}
		for _, j := range js {
			p.EligibilityProofs = append(p.EligibilityProofs, types.VotingEligibility{
				J:   j,
				Sig: vsigner.Sign(proposals.MustSerializeVRFMessage(beacon, epoch, nonce, j)),
			})
		}
	}

}

func expectProposal(signer *signing.EdSigner, lid types.LayerID, atx types.ATXID, opinion types.Opinion, opts ...expectOpt) *types.Proposal {
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

	expectProposal *types.Proposal
	expectErr      string
}

func TestBuild(t *testing.T) {
	signer, err := signing.NewEdSigner(signing.WithKeyFromRand(rand.New(rand.NewSource(10101))))
	require.NoError(t, err)
	defaults := []Opt{
		WithLayerPerEpoch(3),
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
					lid:    10,
					beacon: types.Beacon{1},
					atxs: []*types.VerifiedActivationTx{
						gatx(types.ATXID{1}, 2, signer.NodeID(), 1, genAtxWithNonce(777)),
						gatx(types.ATXID{2}, 2, types.NodeID{2}, 1),
						gatx(types.ATXID{3}, 2, types.NodeID{3}, 1),
						gatx(types.ATXID{4}, 2, types.NodeID{4}, 1),
					},
					opinion:        &types.Opinion{Hash: types.Hash32{1}},
					txs:            []types.TransactionID{{1}, {2}},
					latestComplete: 10,
					expectProposal: expectProposal(
						signer, 10, types.ATXID{1}, types.Opinion{Hash: types.Hash32{1}},
						expectEpochData(
							gactiveset(types.ATXID{1}, types.ATXID{2}, types.ATXID{3}, types.ATXID{4}),
							7,
							types.Beacon{1},
						),
						expectTxs([]types.TransactionID{{1}, {2}}),
						expectCounters(signer, 3, types.Beacon{1}, 777, 4, 5),
					),
				},
			},
		},
		{
			desc: "min active weight",
			opts: []Opt{WithMinimalActiveSetWeight(1000)},
			steps: []step{
				{
					lid:    9,
					beacon: types.Beacon{1},
					atxs: []*types.VerifiedActivationTx{
						gatx(types.ATXID{1}, 2, signer.NodeID(), 1, genAtxWithNonce(777)),
					},
					opinion:        &types.Opinion{Hash: types.Hash32{1}},
					txs:            []types.TransactionID{},
					latestComplete: 8,
					expectProposal: expectProposal(
						signer, 9, types.ATXID{1}, types.Opinion{Hash: types.Hash32{1}},
						expectEpochData(
							gactiveset(types.ATXID{1}),
							3,
							types.Beacon{1},
						),
						expectCounters(signer, 3, types.Beacon{1}, 777, 0, 2),
					),
				},
			},
		},
		{
			desc: "sanity secondary",
			steps: []step{
				{
					lid:    11,
					beacon: types.Beacon{1},
					atxs: []*types.VerifiedActivationTx{
						gatx(types.ATXID{1}, 2, signer.NodeID(), 1, genAtxWithNonce(777)),
						gatx(types.ATXID{2}, 2, types.NodeID{2}, 1),
					},
					ballots: []*types.Ballot{
						gballot(types.BallotID{1}, types.ATXID{1}, signer.NodeID(), 10, &types.EpochData{
							ActiveSetHash:    types.ATXIDList{{1}, {2}}.Hash(),
							EligibilityCount: 5,
						}),
					},
					activeset:      types.ATXIDList{{1}, {2}},
					txs:            []types.TransactionID{},
					opinion:        &types.Opinion{Hash: types.Hash32{1}},
					latestComplete: 10,
					expectProposal: expectProposal(
						signer, 11, types.ATXID{1}, types.Opinion{Hash: types.Hash32{1}},
						expectRef(types.BallotID{1}),
						expectCounters(signer, 3, types.Beacon{1}, 777, 1),
					),
				},
			},
		},
		{
			desc: "no data",
			steps: []step{
				{
					lid:       11,
					expectErr: "atx not available",
				},
				{
					lid: 11,
					atxs: []*types.VerifiedActivationTx{
						gatx(types.ATXID{10}, 2, signer.NodeID(), 1),
					},
					expectErr: "missing nonce",
				},
				{
					lid: 11,
					atxs: []*types.VerifiedActivationTx{
						gatx(types.ATXID{1}, 2, signer.NodeID(), 1, genAtxWithNonce(777)),
					},
					expectErr: "missing beacon",
				},
				{
					lid:    11,
					beacon: types.Beacon{1},
					ballots: []*types.Ballot{
						gballot(types.BallotID{1}, types.ATXID{10}, signer.NodeID(), 10, &types.EpochData{
							ActiveSetHash:    types.ATXIDList{{10}, {2}}.Hash(),
							EligibilityCount: 5,
						}),
					},
					expectErr: "get activeset",
				},
				{
					lid:       11,
					activeset: types.ATXIDList{{10}, {2}},
					expectErr: "get ATXs from DB",
				},
				{
					lid: 11,
					atxs: []*types.VerifiedActivationTx{
						gatx(types.ATXID{2}, 2, types.NodeID{1}, 1),
					},
					opinion:        &types.Opinion{Hash: types.Hash32{1}},
					txs:            []types.TransactionID{{1}},
					latestComplete: 10,
					expectProposal: expectProposal(
						signer, 11, types.ATXID{10}, types.Opinion{Hash: types.Hash32{1}},
						expectRef(types.BallotID{1}),
						expectTxs([]types.TransactionID{{1}}),
						expectCounters(signer, 3, types.Beacon{1}, 777, 1),
					),
				},
			},
		},
		{
			desc: "not eligible",
			steps: []step{
				{
					lid:    11,
					beacon: types.Beacon{1},
					atxs: []*types.VerifiedActivationTx{
						gatx(types.ATXID{1}, 2, signer.NodeID(), 1, genAtxWithNonce(777)),
						gatx(types.ATXID{2}, 2, types.NodeID{2}, 100),
					},
					ballots: []*types.Ballot{
						gballot(types.BallotID{1}, types.ATXID{1}, signer.NodeID(), 10, &types.EpochData{
							ActiveSetHash:    types.ATXIDList{{1}, {2}}.Hash(),
							EligibilityCount: 1,
						}),
					},
					activeset: types.ATXIDList{{1}, {2}},
				},
				{
					lid:       11,
					expectErr: "was already built",
				},
			},
		},
		{
			desc: "encode votes error",
			steps: []step{
				{
					lid:    11,
					beacon: types.Beacon{1},
					atxs: []*types.VerifiedActivationTx{
						gatx(types.ATXID{1}, 2, signer.NodeID(), 1, genAtxWithNonce(777)),
					},
					ballots: []*types.Ballot{
						gballot(types.BallotID{1}, types.ATXID{1}, signer.NodeID(), 10, &types.EpochData{
							ActiveSetHash:    types.ATXIDList{{1}}.Hash(),
							EligibilityCount: 10,
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
					lid:    11,
					beacon: types.Beacon{1},
					atxs: []*types.VerifiedActivationTx{
						gatx(types.ATXID{1}, 2, signer.NodeID(), 1, genAtxWithNonce(777)),
					},
					ballots: []*types.Ballot{
						gballot(types.BallotID{1}, types.ATXID{1}, signer.NodeID(), 10, &types.EpochData{
							ActiveSetHash:    types.ATXIDList{{1}}.Hash(),
							EligibilityCount: 10,
						}),
					},
					activeset:      types.ATXIDList{{1}},
					latestComplete: 10,
					opinion:        &types.Opinion{},
					txs:            []types.TransactionID{},
					publishErr:     errors.New("test publish"),
					expectErr:      "test publish",
				},
			},
		},
		{
			desc: "malicious is not added to activeset",
			steps: []step{
				{
					lid:    10,
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
					latestComplete: 10,
					expectProposal: expectProposal(
						signer, 10, types.ATXID{1}, types.Opinion{Hash: types.Hash32{1}},
						expectEpochData(
							gactiveset(types.ATXID{1}),
							30,
							types.Beacon{1},
						),
						expectCounters(signer, 3, types.Beacon{1}, 777, 4, 5, 8, 13, 18, 19, 21, 24, 25, 26),
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
					lid:    11,
					beacon: types.Beacon{1},
					atxs: []*types.VerifiedActivationTx{
						gatx(types.ATXID{1}, 2, signer.NodeID(), 1, genAtxWithNonce(777)),
						gatx(types.ATXID{2}, 2, types.NodeID{2}, 1, genAtxWithReceived(time.Unix(20, 0))),
						gatx(types.ATXID{3}, 2, types.NodeID{3}, 1, genAtxWithReceived(time.Unix(20, 0))),
					},
					expectErr: "first block",
				},
				{
					lid: 11,
					blocks: []*types.Block{
						gblock(10, types.ATXID{4}), // this atx and ballot doesn't exist
					},
					expectErr: "actives get ballot",
				},
				{
					lid: 11,
					ballots: []*types.Ballot{
						gballot(types.BallotID{11}, types.ATXID{4}, types.NodeID{5}, 10, &types.EpochData{
							ActiveSetHash: gactiveset(types.ATXID{1}, types.ATXID{2}).Hash(),
						}),
					},
					expectErr: "get active hash for ballot",
				},
				{
					lid:       11,
					activeset: gactiveset(types.ATXID{1}, types.ATXID{2}),
					expectErr: "get ATXs from DB: get id 0400000000",
				},
				{
					lid: 11,
					atxs: []*types.VerifiedActivationTx{
						gatx(types.ATXID{4}, 2, types.NodeID{4}, 1, genAtxWithReceived(time.Unix(20, 0))),
					},
					opinion:        &types.Opinion{Hash: types.Hash32{1}},
					txs:            []types.TransactionID{},
					latestComplete: 10,
					expectProposal: expectProposal(
						signer, 11, types.ATXID{1}, types.Opinion{Hash: types.Hash32{1}},
						expectEpochData(
							gactiveset(types.ATXID{1}, types.ATXID{2}, types.ATXID{4}),
							10,
							types.Beacon{1},
						),
						expectCounters(signer, 3, types.Beacon{1}, 777, 1, 6, 7),
					),
				},
			},
		},
		{
			desc: "mesh hash selection",
			opts: []Opt{WithHdist(2)},
			steps: []step{
				{
					lid:    9,
					beacon: types.Beacon{1},
					atxs: []*types.VerifiedActivationTx{
						gatx(types.ATXID{1}, 2, signer.NodeID(), 1, genAtxWithNonce(777)),
					},
					ballots: []*types.Ballot{
						gballot(types.BallotID{1}, types.ATXID{1}, signer.NodeID(), 9, &types.EpochData{
							ActiveSetHash:    types.ATXIDList{{1}}.Hash(),
							EligibilityCount: 10,
						}),
					},
					activeset:      types.ATXIDList{{1}},
					txs:            []types.TransactionID{},
					opinion:        &types.Opinion{Hash: types.Hash32{1}},
					latestComplete: 5, // layers outside hdist not verified
					expectProposal: expectProposal(
						signer, 9, types.ATXID{1}, types.Opinion{Hash: types.Hash32{1}},
						expectRef(types.BallotID{1}),
						expectCounters(signer, 3, types.Beacon{1}, 777, 0, 2, 3, 9),
					),
				},
				{
					lid:            10,
					txs:            []types.TransactionID{},
					opinion:        &types.Opinion{Hash: types.Hash32{1}},
					latestComplete: 8, // missing hare output for layer within hdist
					expectProposal: expectProposal(
						signer, 10, types.ATXID{1}, types.Opinion{Hash: types.Hash32{1}},
						expectRef(types.BallotID{1}),
						expectCounters(signer, 3, types.Beacon{1}, 777, 4, 5, 8),
					),
				},
				{
					lid:            11,
					txs:            []types.TransactionID{},
					opinion:        &types.Opinion{Hash: types.Hash32{1}},
					latestComplete: 8,
					hare:           []types.LayerID{9, 10}, // failed to get mesh hash
					expectProposal: expectProposal(
						signer, 11, types.ATXID{1}, types.Opinion{Hash: types.Hash32{1}},
						expectRef(types.BallotID{1}),
						expectCounters(signer, 3, types.Beacon{1}, 777, 1, 6, 7),
					),
				},
			},
		},
		{
			desc: "mesh hash selection",
			opts: []Opt{WithHdist(6)},
			steps: []step{
				{
					lid:    9,
					beacon: types.Beacon{1},
					atxs: []*types.VerifiedActivationTx{
						gatx(types.ATXID{1}, 2, signer.NodeID(), 1, genAtxWithNonce(777)),
					},
					ballots: []*types.Ballot{
						gballot(types.BallotID{1}, types.ATXID{1}, signer.NodeID(), 9, &types.EpochData{
							ActiveSetHash:    types.ATXIDList{{1}}.Hash(),
							EligibilityCount: 10,
						}),
					},
					activeset:      types.ATXIDList{{1}},
					txs:            []types.TransactionID{},
					opinion:        &types.Opinion{Hash: types.Hash32{1}},
					latestComplete: 8, // genesis case
					expectProposal: expectProposal(
						signer, 9, types.ATXID{1}, types.Opinion{Hash: types.Hash32{1}},
						expectRef(types.BallotID{1}),
						expectCounters(signer, 3, types.Beacon{1}, 777, 0, 2, 3, 9),
					),
				},
				{
					lid:            10,
					txs:            []types.TransactionID{},
					opinion:        &types.Opinion{Hash: types.Hash32{1}},
					latestComplete: 8,
					hare:           []types.LayerID{6, 7, 8, 9},
					aggHashes:      []aggHash{{lid: 9, hash: types.Hash32{9, 9, 9}}},
					expectProposal: expectProposal(
						signer, 10, types.ATXID{1}, types.Opinion{Hash: types.Hash32{1}},
						expectRef(types.BallotID{1}),
						expectMeshHash(types.Hash32{9, 9, 9}),
						expectCounters(signer, 3, types.Beacon{1}, 777, 4, 5, 8),
					),
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

			full := append(defaults, tc.opts...)
			full = append(full, WithLogger(logtest.New(t)))
			builder := New(clock, signer, cdb, publisher, tortoise, syncer, conState, full...)
			for _, step := range tc.steps {
				{
					if step.beacon != types.EmptyBeacon {
						require.NoError(t, beacons.Add(cdb, step.lid.GetEpoch(), step.beacon))
					}
					for _, iden := range step.identitities {
						require.NoError(t, identities.SetMalicious(cdb, iden.id, codec.MustEncode(&iden.proof), iden.received))
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
						require.NoError(t, activesets.Add(cdb, step.activeset.Hash(), &types.EpochActiveSet{Set: step.activeset}))
					}
				}
				{
					if step.opinion != nil {
						tortoise.EXPECT().TallyVotes(ctx, step.lid)
						tortoise.EXPECT().EncodeVotes(ctx, gomock.Any()).Return(step.opinion, step.encodeVotesErr)
					}
					if step.txs != nil {
						conState.EXPECT().SelectProposalTXs(step.lid, gomock.Any()).Return(step.txs)
					}
					if step.latestComplete != 0 {
						tortoise.EXPECT().LatestComplete().Return(step.latestComplete)
					}
				}
				var decoded *types.Proposal
				if step.expectProposal != nil || step.publishErr != nil {
					publisher.EXPECT().Publish(ctx, pubsub.ProposalProtocol, gomock.Any()).DoAndReturn(func(_ context.Context, _ string, msg []byte) error {
						var proposal types.Proposal
						codec.MustDecode(msg, &proposal)
						proposal.MustInitialize()
						decoded = &proposal
						return step.publishErr
					})
				}
				err := builder.build(ctx, step.lid)
				if len(step.expectErr) > 0 {
					require.ErrorContains(t, err, step.expectErr)
				} else {
					require.NoError(t, err)
					if step.expectProposal != nil {
						require.Equal(t, *step.expectProposal, *decoded)
					} else {
						require.Nil(t, decoded)
					}
				}
			}
		})
	}
}
