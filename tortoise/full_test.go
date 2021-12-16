package tortoise

import (
	"math/big"
	mrand "math/rand"
	"testing"

	"github.com/golang/mock/gomock"
	"github.com/stretchr/testify/require"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/log/logtest"
	"github.com/spacemeshos/go-spacemesh/signing"
	"github.com/spacemeshos/go-spacemesh/tortoise/mocks"
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
			expect:           true,
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
			expect:    false,
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
			expect:    true,
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

			require.Equal(t, tc.expect, f.ballotFilter(tc.ballot, tc.ballotlid))
		})
	}
}

func TestFullCountVotes(t *testing.T) {
	type testBallot struct {
		Base                      [2]int   // [layer, ballot] tuple
		Support, Against, Abstain [][2]int // list of [layer, block] tuples
		ATX                       int
	}
	type testBlock struct{}

	rng := mrand.New(mrand.NewSource(0))
	signer := signing.NewEdSignerFromRand(rng)

	getDiff := func(layers [][]*types.Block, choices [][2]int) []types.BlockID {
		var rst []types.BlockID
		for _, choice := range choices {
			rst = append(rst, layers[choice[0]][choice[1]].ID())
		}
		return rst
	}

	genesis := types.GetEffectiveGenesis()

	for _, tc := range []struct {
		desc         string
		activeset    []uint         // list of weights in activeset
		layerBallots [][]testBallot // list of layers with ballots
		layerBlocks  [][]testBlock
		target       [2]int // [layer, block] tuple
		expect       *big.Float
	}{
		{
			desc:      "TwoLayersSupport",
			activeset: []uint{10, 10, 10},
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
			expect: big.NewFloat(15),
		},
		{
			desc:      "ConflictWithBase",
			activeset: []uint{10, 10, 10},
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
			expect: big.NewFloat(0),
		},
		{
			desc:      "UnequalWeights",
			activeset: []uint{80, 40, 20},
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
			expect: big.NewFloat(140),
		},
		{
			desc:      "UnequalWeightsVoteFromAtxMissing",
			activeset: []uint{80, 40, 20},
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
			expect: big.NewFloat(100),
		},
		{
			desc:      "OneLayerSupport",
			activeset: []uint{10, 10, 10},
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
			expect: big.NewFloat(7.5),
		},
		{
			desc:      "OneBlockAbstain",
			activeset: []uint{10, 10, 10},
			layerBlocks: [][]testBlock{
				{{}, {}, {}},
			},
			layerBallots: [][]testBallot{
				{{ATX: 0}, {ATX: 1}, {ATX: 2}},
				{
					{ATX: 0, Base: [2]int{0, 1}, Support: [][2]int{{0, 1}, {0, 0}, {0, 2}}},
					{ATX: 1, Base: [2]int{0, 1}, Support: [][2]int{{0, 1}, {0, 0}, {0, 2}}},
					{ATX: 2, Base: [2]int{0, 1}, Abstain: [][2]int{{0, 1}, {0, 0}, {0, 2}}},
				},
			},
			target: [2]int{0, 0},
			expect: big.NewFloat(5),
		},
		{
			desc:      "OneBlockAagaisnt",
			activeset: []uint{10, 10, 10},
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
			expect: big.NewFloat(2.5),
		},
		{
			desc:      "MajorityAgainst",
			activeset: []uint{10, 10, 10},
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
			expect: big.NewFloat(-2.5),
		},
		{
			desc:      "NoVotes",
			activeset: []uint{10, 10, 10},
			layerBlocks: [][]testBlock{
				{{}, {}, {}},
			},
			layerBallots: [][]testBallot{
				{{ATX: 0}, {ATX: 1}, {ATX: 2}},
			},
			target: [2]int{0, 0},
			expect: big.NewFloat(0),
		},
	} {
		tc := tc
		t.Run(tc.desc, func(t *testing.T) {
			logger := logtest.New(t)
			ctrl := gomock.NewController(t)
			atxdb := mocks.NewMockatxDataProvider(ctrl)
			activeset := []types.ATXID{}
			for i, weight := range tc.activeset {
				header := makeAtxHeaderWithWeight(weight)
				atxid := types.ATXID{byte(i)}
				header.SetID(&atxid)
				atxdb.EXPECT().GetAtxHeader(atxid).Return(header, nil).AnyTimes()
				activeset = append(activeset, atxid)
			}

			tortoise := defaultAlgorithm(t, getInMemMesh(t))
			tortoise.trtl.atxdb = atxdb
			consensus := tortoise.trtl

			blocks := [][]*types.Block{}
			for i, layer := range tc.layerBlocks {
				layerBlocks := []*types.Block{}
				lid := genesis.Add(uint32(i) + 1)
				for j := range layer {
					p := &types.Proposal{}
					p.EligibilityProof = types.VotingEligibilityProof{J: uint32(j)}
					p.LayerIndex = lid
					p.Ballot.Signature = signer.Sign(p.Ballot.Bytes())
					p.Signature = signer.Sign(p.Bytes())
					require.NoError(t, p.Initialize())
					layerBlocks = append(layerBlocks, (*types.Block)(p))
				}

				consensus.processBlocks(lid, layerBlocks)
				consensus.full.processBlocks(layerBlocks)
				blocks = append(blocks, layerBlocks)
			}

			ballots := [][]*types.Ballot{}
			for i, layer := range tc.layerBallots {
				layerBallots := []*types.Ballot{}
				lid := genesis.Add(uint32(i) + 1)
				for j, b := range layer {
					ballot := &types.Ballot{}
					ballot.EligibilityProof = types.VotingEligibilityProof{J: uint32(j)}
					ballot.AtxID = activeset[b.ATX]
					ballot.EpochData = &types.EpochData{ActiveSet: activeset}
					ballot.LayerIndex = lid
					// don't vote on genesis for simplicity,
					// since we don't care about block goodness in this test
					if i > 0 {
						ballot.ForDiff = getDiff(blocks, b.Support)
						ballot.AgainstDiff = getDiff(blocks, b.Against)
						ballot.NeutralDiff = getDiff(blocks, b.Abstain)
						ballot.BaseBallot = ballots[b.Base[0]][b.Base[1]].ID()
					}
					ballot.Signature = signer.Sign(ballot.Bytes())
					require.NoError(t, ballot.Initialize())
					layerBallots = append(layerBallots, ballot)
				}
				ballots = append(ballots, layerBallots)

				consensus.processed = lid
				consensus.last = lid
				tballots, err := consensus.processBallots(lid, layerBallots)
				consensus.full.processBallots(tballots)
				require.NoError(t, err)
				consensus.full.processBallots(tballots)

				consensus.full.countVotes(logger)
			}
			bid := blocks[tc.target[0]][tc.target[1]].ID()
			require.Equal(t, tc.expect.String(), consensus.full.weights[bid].String())
		})
	}
}
