package tortoise

import (
	mrand "math/rand"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/common/util"
	"github.com/spacemeshos/go-spacemesh/datastore"
	"github.com/spacemeshos/go-spacemesh/log/logtest"
	"github.com/spacemeshos/go-spacemesh/signing"
	"github.com/spacemeshos/go-spacemesh/sql"
	"github.com/spacemeshos/go-spacemesh/sql/atxs"
)

func TestFullBallotFilter(t *testing.T) {
	for _, tc := range []struct {
		desc             string
		badBeaconBallots map[types.BallotID]struct{}
		distance         uint32
		ballot           types.BallotID
		ballotlid        types.LayerID
		last             types.LayerID
		expect           bool
	}{
		{
			desc:             "Good",
			badBeaconBallots: map[types.BallotID]struct{}{},
			ballot:           types.BallotID{1},
			expect:           false,
		},
		{
			desc: "BadFromRecent",
			badBeaconBallots: map[types.BallotID]struct{}{
				{1}: {},
			},
			ballot:    types.BallotID{1},
			ballotlid: types.NewLayerID(10),
			last:      types.NewLayerID(11),
			distance:  2,
			expect:    true,
		},
		{
			desc: "BadFromOld",
			badBeaconBallots: map[types.BallotID]struct{}{
				{1}: {},
			},
			ballot:    types.BallotID{1},
			ballotlid: types.NewLayerID(8),
			last:      types.NewLayerID(11),
			distance:  2,
			expect:    false,
		},
	} {
		tc := tc
		t.Run(tc.desc, func(t *testing.T) {
			state := newCommonState()
			state.badBeaconBallots = tc.badBeaconBallots
			state.last = tc.last

			config := Config{}
			config.BadBeaconVoteDelayLayers = tc.distance

			f := newFullTortoise(config, &state)

			require.Equal(t, tc.expect, f.shouldBeDelayed(tc.ballot, tc.ballotlid))
		})
	}
}

func TestFullCountVotes(t *testing.T) {
	type testBallot struct {
		Base             [2]int   // [layer, ballot] tuple
		Support, Against [][2]int // list of [layer, block] tuples
		Abstain          []int    // [layer]
		ATX              int
	}
	type testBlock struct {
		Height uint64
	}
	type testAtx struct {
		BaseHeight, TickCount uint64
	}
	const localHeight = 100
	rng := mrand.New(mrand.NewSource(0))
	signer := signing.NewEdSignerFromRand(rng)

	getDiff := func(layers [][]types.Block, choices [][2]int) []types.BlockID {
		var rst []types.BlockID
		for _, choice := range choices {
			rst = append(rst, layers[choice[0]][choice[1]].ID())
		}
		return rst
	}

	genesis := types.GetEffectiveGenesis()

	for _, tc := range []struct {
		desc         string
		activeset    []testAtx      // list of atxs
		layerBallots [][]testBallot // list of layers with ballots
		layerBlocks  [][]testBlock
		target       [2]int // [layer, block] tuple
		expect       util.Weight
	}{
		{
			desc:      "TwoLayersSupport",
			activeset: []testAtx{{TickCount: 10}, {TickCount: 10}, {TickCount: 10}},
			layerBlocks: [][]testBlock{
				{{}, {}, {}},
				{{}, {}, {}},
			},
			layerBallots: [][]testBallot{
				{{ATX: 0}, {ATX: 1}, {ATX: 2}},
				{
					{ATX: 0, Base: [2]int{0, 1}, Support: [][2]int{{0, 1}, {0, 0}, {0, 2}}},
					{ATX: 1, Base: [2]int{0, 1}, Support: [][2]int{{0, 1}, {0, 0}, {0, 2}}},
					{ATX: 2, Base: [2]int{0, 1}, Support: [][2]int{{0, 1}, {0, 0}, {0, 2}}},
				},
				{
					{ATX: 0, Base: [2]int{1, 1}, Support: [][2]int{{1, 1}, {1, 0}, {1, 2}}},
					{ATX: 1, Base: [2]int{1, 1}, Support: [][2]int{{1, 1}, {1, 0}, {1, 2}}},
					{ATX: 2, Base: [2]int{1, 1}, Support: [][2]int{{1, 1}, {1, 0}, {1, 2}}},
				},
			},
			target: [2]int{0, 0},
			expect: util.WeightFromFloat64(15),
		},
		{
			desc:      "ConflictWithBase",
			activeset: []testAtx{{TickCount: 10}, {TickCount: 10}, {TickCount: 10}},
			layerBlocks: [][]testBlock{
				{{}, {}, {}},
				{{}, {}, {}},
			},
			layerBallots: [][]testBallot{
				{{ATX: 0}, {ATX: 1}, {ATX: 2}},
				{
					{ATX: 0, Base: [2]int{0, 1}, Support: [][2]int{{0, 1}, {0, 0}, {0, 2}}},
					{ATX: 1, Base: [2]int{0, 1}, Support: [][2]int{{0, 1}, {0, 0}, {0, 2}}},
					{ATX: 2, Base: [2]int{0, 1}, Support: [][2]int{{0, 1}, {0, 0}, {0, 2}}},
				},
				{
					{
						ATX: 0, Base: [2]int{1, 1},
						Support: [][2]int{{1, 1}, {1, 0}, {1, 2}},
						Against: [][2]int{{0, 1}, {0, 0}, {0, 2}},
					},
					{
						ATX: 1, Base: [2]int{1, 1},
						Support: [][2]int{{1, 1}, {1, 0}, {1, 2}},
						Against: [][2]int{{0, 1}, {0, 0}, {0, 2}},
					},
					{
						ATX: 2, Base: [2]int{1, 1},
						Support: [][2]int{{1, 1}, {1, 0}, {1, 2}},
						Against: [][2]int{{0, 1}, {0, 0}, {0, 2}},
					},
				},
			},
			target: [2]int{0, 0},
			expect: util.WeightFromFloat64(0),
		},
		{
			desc:      "UnequalWeights",
			activeset: []testAtx{{TickCount: 80}, {TickCount: 40}, {TickCount: 20}},
			layerBlocks: [][]testBlock{
				{{}, {}, {}},
				{{}, {}, {}},
				{{}, {}, {}},
			},
			layerBallots: [][]testBallot{
				{{ATX: 0}, {ATX: 1}, {ATX: 2}},
				{
					{ATX: 0, Base: [2]int{0, 1}, Support: [][2]int{{0, 1}, {0, 0}, {0, 2}}},
					{ATX: 1, Base: [2]int{0, 1}, Support: [][2]int{{0, 1}, {0, 0}, {0, 2}}},
					{ATX: 2, Base: [2]int{0, 1}, Support: [][2]int{{0, 1}, {0, 0}, {0, 2}}},
				},
				{
					{ATX: 0, Base: [2]int{1, 1}, Support: [][2]int{{1, 1}, {1, 0}, {1, 2}}},
					{ATX: 0, Base: [2]int{1, 1}, Support: [][2]int{{1, 1}, {1, 0}, {1, 2}}},
					{ATX: 1, Base: [2]int{1, 1}, Support: [][2]int{{1, 1}, {1, 0}, {1, 2}}},
				},
				{
					{ATX: 0, Base: [2]int{2, 1}, Support: [][2]int{{2, 1}, {2, 0}, {2, 2}}},
					{ATX: 0, Base: [2]int{2, 1}, Support: [][2]int{{2, 1}, {2, 0}, {2, 2}}},
					{ATX: 0, Base: [2]int{2, 1}, Support: [][2]int{{2, 1}, {2, 0}, {2, 2}}},
					{ATX: 1, Base: [2]int{2, 1}, Support: [][2]int{{2, 1}, {2, 0}, {2, 2}}},
				},
			},
			target: [2]int{0, 0},
			expect: util.WeightFromFloat64(140),
		},
		{
			desc:      "UnequalWeightsVoteFromAtxMissing",
			activeset: []testAtx{{TickCount: 80}, {TickCount: 40}, {TickCount: 20}},
			layerBlocks: [][]testBlock{
				{{}, {}, {}},
				{{}, {}, {}},
				{{}, {}, {}},
			},
			layerBallots: [][]testBallot{
				{{ATX: 0}, {ATX: 1}, {ATX: 2}},
				{
					{ATX: 0, Base: [2]int{0, 1}, Support: [][2]int{{0, 1}, {0, 0}, {0, 2}}},
					{ATX: 2, Base: [2]int{0, 1}, Support: [][2]int{{0, 1}, {0, 0}, {0, 2}}},
				},
				{
					{ATX: 0, Base: [2]int{1, 1}, Support: [][2]int{{1, 1}, {1, 0}}},
					{ATX: 0, Base: [2]int{1, 1}, Support: [][2]int{{1, 1}, {1, 0}}},
				},
				{
					{ATX: 0, Base: [2]int{2, 1}, Support: [][2]int{{2, 1}, {2, 0}}},
					{ATX: 0, Base: [2]int{2, 1}, Support: [][2]int{{2, 1}, {2, 0}}},
					{ATX: 0, Base: [2]int{2, 1}, Support: [][2]int{{2, 1}, {2, 0}}},
				},
			},
			target: [2]int{0, 0},
			expect: util.WeightFromFloat64(100),
		},
		{
			desc:      "OneLayerSupport",
			activeset: []testAtx{{TickCount: 10}, {TickCount: 10}, {TickCount: 10}},
			layerBlocks: [][]testBlock{
				{{}, {}, {}},
			}, layerBallots: [][]testBallot{
				{{ATX: 0}, {ATX: 1}, {ATX: 2}},
				{
					{ATX: 0, Base: [2]int{0, 1}, Support: [][2]int{{0, 1}, {0, 0}, {0, 2}}},
					{ATX: 1, Base: [2]int{0, 1}, Support: [][2]int{{0, 1}, {0, 0}, {0, 2}}},
					{ATX: 2, Base: [2]int{0, 1}, Support: [][2]int{{0, 1}, {0, 0}, {0, 2}}},
				},
			},
			target: [2]int{0, 0},
			expect: util.WeightFromFloat64(7.5),
		},
		{
			desc:      "OneBlockAbstain",
			activeset: []testAtx{{TickCount: 10}, {TickCount: 10}, {TickCount: 10}},
			layerBlocks: [][]testBlock{
				{{}, {}, {}},
			},
			layerBallots: [][]testBallot{
				{{ATX: 0}, {ATX: 1}, {ATX: 2}},
				{
					{ATX: 0, Base: [2]int{0, 1}, Support: [][2]int{{0, 1}, {0, 0}, {0, 2}}},
					{ATX: 1, Base: [2]int{0, 1}, Support: [][2]int{{0, 1}, {0, 0}, {0, 2}}},
					{ATX: 2, Base: [2]int{0, 1}, Abstain: []int{0}},
				},
			},
			target: [2]int{0, 0},
			expect: util.WeightFromFloat64(5),
		},
		{
			desc:      "OneBlockAagaisnt",
			activeset: []testAtx{{TickCount: 10}, {TickCount: 10}, {TickCount: 10}},
			layerBlocks: [][]testBlock{
				{{}, {}, {}},
			},
			layerBallots: [][]testBallot{
				{{ATX: 0}, {ATX: 1}, {ATX: 2}},
				{
					{ATX: 0, Base: [2]int{0, 1}, Support: [][2]int{{0, 1}, {0, 0}, {0, 2}}},
					{ATX: 1, Base: [2]int{0, 1}, Support: [][2]int{{0, 1}, {0, 0}, {0, 2}}},
					{ATX: 2, Base: [2]int{0, 1}, Against: [][2]int{{0, 1}, {0, 0}, {0, 2}}},
				},
			},
			target: [2]int{0, 0},
			expect: util.WeightFromFloat64(2.5),
		},
		{
			desc:      "MajorityAgainst",
			activeset: []testAtx{{TickCount: 10}, {TickCount: 10}, {TickCount: 10}},
			layerBlocks: [][]testBlock{
				{{}, {}, {}},
			},
			layerBallots: [][]testBallot{
				{{ATX: 0}, {ATX: 1}, {ATX: 2}},
				{
					{ATX: 0, Base: [2]int{0, 1}, Support: [][2]int{{0, 1}, {0, 0}, {0, 2}}},
					{ATX: 1, Base: [2]int{0, 1}, Against: [][2]int{{0, 1}, {0, 0}, {0, 2}}},
					{ATX: 2, Base: [2]int{0, 1}, Against: [][2]int{{0, 1}, {0, 0}, {0, 2}}},
				},
			},
			target: [2]int{0, 0},
			expect: util.WeightFromFloat64(-2.5),
		},
		{
			desc:      "NoVotes",
			activeset: []testAtx{{TickCount: 10}, {TickCount: 10}, {TickCount: 10}},
			layerBlocks: [][]testBlock{
				{{}, {}, {}},
			},
			layerBallots: [][]testBallot{
				{{ATX: 0}, {ATX: 1}, {ATX: 2}},
			},
			target: [2]int{0, 0},
			expect: util.WeightFromFloat64(0),
		},
		{
			desc:      "FutureVotes",
			activeset: []testAtx{{TickCount: 10}, {TickCount: 12}},
			layerBlocks: [][]testBlock{
				{{Height: 11}},
			},
			layerBallots: [][]testBallot{
				{{ATX: 0}, {ATX: 1}},
				{
					{ATX: 0, Base: [2]int{0, 1}, Support: [][2]int{{0, 0}}}, // ignored
					{ATX: 1, Base: [2]int{0, 1}, Support: [][2]int{{0, 0}}}, // counted
				},
				{
					{ATX: 0, Base: [2]int{1, 1}}, // ignored
					{ATX: 1, Base: [2]int{1, 0}}, // counted regardless of the base ballot choice
				},
			},
			target: [2]int{0, 0},
			expect: util.WeightFromFloat64(4),
		},
	} {
		tc := tc
		t.Run(tc.desc, func(t *testing.T) {
			logger := logtest.New(t)
			cdb := datastore.NewCachedDB(sql.InMemory(), logger)
			var activeset []types.ATXID
			for i := range tc.activeset {
				atx := &types.ActivationTx{InnerActivationTx: types.InnerActivationTx{
					NIPostChallenge: types.NIPostChallenge{},
					NumUnits:        1,
				}}
				atxid := types.ATXID{byte(i + 1)}
				atx.SetID(&atxid)
				atx.SetNodeID(&types.NodeID{1})
				vAtx := atx.Verify(tc.activeset[i].BaseHeight, tc.activeset[i].TickCount)
				require.NoError(t, atxs.Add(cdb, vAtx, time.Now()))
				activeset = append(activeset, atxid)
			}

			tortoise := defaultAlgorithm(t, cdb)
			tortoise.trtl.cdb = cdb
			consensus := tortoise.trtl

			var blocks [][]types.Block
			for i, layer := range tc.layerBlocks {
				var layerBlocks []types.Block
				lid := genesis.Add(uint32(i) + 1)
				for j := range layer {
					b := types.Block{}
					b.LayerIndex = lid
					b.TickHeight = layer[j].Height
					b.TxIDs = types.RandomTXSet(j)
					b.Initialize()
					layerBlocks = append(layerBlocks, b)
				}
				consensus.referenceHeight[lid.GetEpoch()] = localHeight
				for _, block := range layerBlocks {
					consensus.onBlock(lid, &block)
				}
				blocks = append(blocks, layerBlocks)
			}

			var ballotsList [][]*types.Ballot
			for i, layer := range tc.layerBallots {
				var layerBallots []*types.Ballot
				lid := genesis.Add(uint32(i) + 1)
				for j, b := range layer {
					ballot := &types.Ballot{}
					ballot.EligibilityProofs = []types.VotingEligibilityProof{{J: uint32(j)}}
					ballot.AtxID = activeset[b.ATX]
					ballot.EpochData = &types.EpochData{ActiveSet: activeset}
					ballot.LayerIndex = lid
					// don't vote on genesis for simplicity,
					// since we don't care about block goodness in this test
					if i > 0 {
						ballot.Votes.Support = getDiff(blocks, b.Support)
						ballot.Votes.Against = getDiff(blocks, b.Against)
						for _, layerNumber := range b.Abstain {
							ballot.Votes.Abstain = append(ballot.Votes.Abstain, genesis.Add(uint32(layerNumber)+1))
						}
						ballot.Votes.Base = ballotsList[b.Base[0]][b.Base[1]].ID()
					}
					ballot.Signature = signer.Sign(ballot.Bytes())
					require.NoError(t, ballot.Initialize())
					layerBallots = append(layerBallots, ballot)
				}
				ballotsList = append(ballotsList, layerBallots)

				consensus.processed = lid
				consensus.last = lid
				for _, ballot := range layerBallots {
					require.NoError(t, consensus.onBallot(ballot))
				}

				consensus.full.countVotes(logger)
			}
			block := blocks[tc.target[0]][tc.target[1]]
			var target *blockInfo
			for _, info := range consensus.blocks[block.LayerIndex] {
				if info.id == block.ID() {
					target = &info
				}
			}
			require.NotNil(t, target)
			require.Equal(t, tc.expect.String(), target.weight.String())
		})
	}
}

func TestFullVerify(t *testing.T) {
	const emptyThreshold = 20
	type testBlock struct {
		height, margin int
	}
	for _, tc := range []struct {
		desc      string
		blocks    []testBlock
		threshold uint64
		validity  []sign
	}{
		{
			desc:      "support",
			blocks:    []testBlock{{margin: 11}},
			threshold: 10,
			validity:  []sign{support},
		},
		{
			desc:      "abstain",
			blocks:    []testBlock{{margin: 10}},
			threshold: emptyThreshold + 1,
		},
		{
			desc: "abstain before support",
			blocks: []testBlock{
				{margin: 10, height: 10},
				{margin: 11, height: 20},
			},
			threshold: emptyThreshold + 1,
		},
		{
			desc: "abstain after support",
			blocks: []testBlock{
				{margin: 10, height: 30},
				{margin: 11, height: 20},
			},
			threshold: 10,
			validity:  []sign{against, support},
		},
		{
			desc: "abstained same height",
			blocks: []testBlock{
				{margin: 11, height: 20},
				{margin: 10, height: 20},
			},
			threshold: emptyThreshold + 1,
		},
		{
			desc: "support after against",
			blocks: []testBlock{
				{margin: -11, height: 10},
				{margin: 11, height: 20},
			},
			threshold: 10,
			validity:  []sign{against, support},
		},
		{
			desc: "only against",
			blocks: []testBlock{
				{margin: -11, height: 10},
				{margin: -11, height: 20},
			},
			threshold: 10,
			validity:  []sign{against, against},
		},
		{
			desc: "support same height",
			blocks: []testBlock{
				{margin: 11, height: 10},
				{margin: 11, height: 10},
			},
			threshold: 10,
			validity:  []sign{support, support},
		},
		{
			desc: "support different height",
			blocks: []testBlock{
				{margin: 11, height: 10},
				{margin: 11, height: 20},
			},
			threshold: 10,
			validity:  []sign{support, support},
		},
		{
			desc: "support abstain support",
			blocks: []testBlock{
				{margin: 11, height: 10},
				{margin: 10, height: 11},
				{margin: 11, height: 20},
			},
			threshold: 10,
			validity:  []sign{support, against, support},
		},
		{
			desc: "against abstain support",
			blocks: []testBlock{
				{margin: -11, height: 10},
				{margin: 10, height: 10},
				{margin: 11, height: 10},
			},
			threshold: emptyThreshold + 1,
		},
		{
			desc:      "empty layer",
			threshold: 10,
			validity:  []sign{},
		},
		{
			desc:      "empty layer not verified",
			threshold: emptyThreshold + 1,
		},
	} {
		t.Run(tc.desc, func(t *testing.T) {
			lid := types.LayerID{}
			full := newFullTortoise(Config{}, &commonState{
				blocks:   map[types.LayerID][]blockInfo{},
				validity: votes{},
			})
			full.globalThreshold = util.WeightFromUint64(tc.threshold)
			full.empty[lid] = util.WeightFromInt64(int64(emptyThreshold))
			for i, block := range tc.blocks {
				id := types.BlockID{uint8(i) + 1}
				full.blocks[lid] = append(full.blocks[lid], blockInfo{
					id:     id,
					height: uint64(block.height),
					weight: util.WeightFromInt64(int64(block.margin)),
				})
			}
			require.Equal(t, tc.validity != nil, full.verify(logtest.New(t), lid))
			if tc.validity != nil {
				for i, expect := range tc.validity {
					id := types.BlockID{uint8(i) + 1}
					require.Equal(t, expect, full.validity[id])
				}
			}
		})
	}
}
