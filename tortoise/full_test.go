package tortoise

import (
	"math/rand"
	"testing"

	"github.com/spacemeshos/fixed"
	"github.com/stretchr/testify/require"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/log/logtest"
	"github.com/spacemeshos/go-spacemesh/signing"
)

func TestFullBallotFilter(t *testing.T) {
	for _, tc := range []struct {
		desc     string
		distance uint32
		ballot   ballotInfo
		last     types.LayerID
		expect   bool
	}{
		{
			desc: "Good",
			ballot: ballotInfo{
				id: types.BallotID{1},
			},
			expect: false,
		},
		{
			desc: "BadFromRecent",
			ballot: ballotInfo{
				id:    types.BallotID{1},
				layer: types.LayerID(10),
				conditions: conditions{
					badBeacon: true,
				},
			},
			last:     types.LayerID(11),
			distance: 2,
			expect:   true,
		},
		{
			desc: "BadFromOld",
			ballot: ballotInfo{
				id:    types.BallotID{1},
				layer: types.LayerID(8),
				conditions: conditions{
					badBeacon: true,
				},
			},
			last:     types.LayerID(11),
			distance: 2,
			expect:   false,
		},
	} {
		tc := tc
		t.Run(tc.desc, func(t *testing.T) {
			state := newState()
			state.last = tc.last
			config := Config{}
			config.BadBeaconVoteDelayLayers = tc.distance
			require.Equal(t, tc.expect, newFullTortoise(config, state).shouldBeDelayed(
				logtest.New(t), &tc.ballot))
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
	rng := rand.New(rand.NewSource(0))
	signer, err := signing.NewEdSigner(
		signing.WithKeyFromRand(rng),
	)
	require.NoError(t, err)

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
		expect       weight
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
			expect: fixed.From(15),
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
			expect: fixed.From(0),
		},
		{
			desc:      "UnequalWeights",
			activeset: []testAtx{{TickCount: 90}, {TickCount: 60}, {TickCount: 30}},
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
			expect: fixed.From(150),
		},
		{
			desc:      "UnequalWeightsVoteFromAtxMissing",
			activeset: []testAtx{{TickCount: 90}, {TickCount: 60}, {TickCount: 30}},
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
			expect: fixed.From(105),
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
			expect: fixed.From(7.5),
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
			expect: fixed.From(5),
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
			expect: fixed.From(2.5),
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
			expect: fixed.From(-2.5),
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
			expect: fixed.From(0),
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
			expect: fixed.From(4),
		},
	} {
		tc := tc
		t.Run(tc.desc, func(t *testing.T) {
			logger := logtest.New(t)
			tortoise := defaultAlgorithm(t)
			var activeset []types.ATXID
			for i := range tc.activeset {
				atxid := types.ATXID{byte(i + 1)}
				header := &types.ActivationTxHeader{
					ID:                atxid,
					NumUnits:          1,
					EffectiveNumUnits: 1,
					BaseTickHeight:    tc.activeset[i].BaseHeight,
					TickCount:         tc.activeset[i].TickCount,
				}
				header.PublishEpoch = 1
				tortoise.OnAtx(header.ToData())
				activeset = append(activeset, atxid)
			}

			consensus := tortoise.trtl
			consensus.ballotRefs[types.EmptyBallotID] = &ballotInfo{
				layer: genesis,
			}

			var blocks [][]types.Block
			refs := map[types.BlockID]types.Vote{}
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
					refs[b.ID()] = b.ToVote()
				}
				consensus.epoch(lid.GetEpoch()).height = localHeight
				for _, block := range layerBlocks {
					tortoise.OnBlock(block.ToVote())
				}
				blocks = append(blocks, layerBlocks)
			}

			var ballotsList [][]*types.Ballot
			for i, layer := range tc.layerBallots {
				var layerBallots []*types.Ballot
				lid := genesis.Add(uint32(i) + 1)
				for j, b := range layer {
					ballot := &types.Ballot{}
					ballot.EligibilityProofs = []types.VotingEligibility{{J: uint32(j)}}
					ballot.AtxID = activeset[b.ATX]
					ballot.EpochData = &types.EpochData{ActiveSetHash: types.Hash32{1, 2, 3}, EligibilityCount: 1}
					ballot.ActiveSet = activeset
					ballot.Layer = lid
					// don't vote on genesis for simplicity,
					// since we don't care about block goodness in this test
					if i > 0 {
						for _, support := range getDiff(blocks, b.Support) {
							ballot.Votes.Support = append(ballot.Votes.Support, refs[support])
						}
						for _, against := range getDiff(blocks, b.Against) {
							ballot.Votes.Against = append(ballot.Votes.Against, refs[against])
						}
						for _, layerNumber := range b.Abstain {
							ballot.Votes.Abstain = append(ballot.Votes.Abstain, genesis.Add(uint32(layerNumber)+1))
						}
						ballot.Votes.Base = ballotsList[b.Base[0]][b.Base[1]].ID()
					}
					ballot.OpinionHash = types.RandomHash() // fake opinion, only to make sure each ballot has a unique ID
					ballot.Signature = signer.Sign(signing.BALLOT, ballot.SignedBytes())
					ballot.SmesherID = signer.NodeID()
					require.NoError(t, ballot.Initialize())
					layerBallots = append(layerBallots, ballot)
				}
				ballotsList = append(ballotsList, layerBallots)

				consensus.processed = lid
				consensus.last = lid
				for _, ballot := range layerBallots {
					require.NoError(t, consensus.onBallot(ballot.ToTortoiseData()))
				}
				consensus.full.countVotes(logger)
			}
			block := blocks[tc.target[0]][tc.target[1]]
			var target *blockInfo
			for _, info := range consensus.layer(block.LayerIndex).blocks {
				if info.id == block.ID() {
					target = info
				}
			}
			require.NotNil(t, target)
			require.Equal(t, tc.expect.String(), target.margin.String())
		})
	}
}

func TestFullVerify(t *testing.T) {
	epoch := types.EpochID(1)
	target := epoch.FirstLayer().Sub(1)
	last := target.Add(types.GetLayersPerEpoch())
	epochs := map[types.EpochID]*epochInfo{
		1: {weight: fixed.From(10)},
	}
	positive := 7
	negative := -7
	neutral := 3
	type testBlock struct {
		height, margin int
	}
	for _, tc := range []struct {
		desc   string
		blocks []testBlock
		empty  int

		validity []sign
	}{
		{
			desc:     "support",
			blocks:   []testBlock{{margin: positive}},
			validity: []sign{support},
		},
		{
			desc:   "abstain",
			blocks: []testBlock{{margin: neutral}},
		},
		{
			desc: "abstain before support",
			blocks: []testBlock{
				{margin: neutral, height: 10},
				{margin: positive, height: 20},
			},
		},
		{
			desc: "abstain after support",
			blocks: []testBlock{
				{margin: neutral, height: 30},
				{margin: positive, height: 20},
			},
			validity: []sign{support, against},
		},
		{
			desc: "abstained same height",
			blocks: []testBlock{
				{margin: positive, height: 20},
				{margin: neutral, height: 20},
			},
		},
		{
			desc: "support after against",
			blocks: []testBlock{
				{margin: negative, height: 10},
				{margin: positive, height: 20},
			},
			validity: []sign{against, support},
		},
		{
			desc: "only against",
			blocks: []testBlock{
				{margin: negative, height: 10},
				{margin: negative, height: 20},
			},
			validity: []sign{against, against},
		},
		{
			desc: "support same height",
			blocks: []testBlock{
				{margin: positive, height: 10},
				{margin: positive, height: 10},
			},
			validity: []sign{support, support},
		},
		{
			desc: "support different height",
			blocks: []testBlock{
				{margin: positive, height: 10},
				{margin: positive, height: 20},
			},
			validity: []sign{support, support},
		},
		{
			desc: "support abstain support",
			blocks: []testBlock{
				{margin: positive, height: 10},
				{margin: neutral, height: 11},
				{margin: positive, height: 20},
			},
			validity: []sign{support, against, support},
		},
		{
			desc: "against abstain support",
			blocks: []testBlock{
				{margin: negative, height: 10},
				{margin: neutral, height: 10},
				{margin: positive, height: 10},
			},
		},
		{
			desc:     "empty layer",
			empty:    positive,
			validity: []sign{},
		},
		{
			desc:  "empty layer not verified",
			empty: neutral,
		},
	} {
		t.Run(tc.desc, func(t *testing.T) {
			full := newFullTortoise(Config{}, newState())
			full.epochs = epochs
			full.last = last
			full.processed = last

			layer := full.layer(target)
			layer.empty = fixed.From(float64(tc.empty))
			for i, block := range tc.blocks {
				block := &blockInfo{
					id:     types.BlockID{uint8(i) + 1},
					layer:  target,
					height: uint64(block.height),
					margin: fixed.From(float64(block.margin)),
				}
				layer.blocks = append(layer.blocks, block)
			}
			require.Equal(t, tc.validity != nil, full.verify(logtest.New(t), target))
			if tc.validity != nil {
				for i, expect := range tc.validity {
					require.Equal(t, expect, layer.blocks[i].validity)
				}
			}
		})
	}
}
