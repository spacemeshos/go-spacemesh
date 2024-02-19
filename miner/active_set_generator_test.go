package miner

import (
	"testing"
	"time"

	"github.com/spacemeshos/go-spacemesh/atxsdata"
	"github.com/spacemeshos/go-spacemesh/codec"
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/log/logtest"
	"github.com/spacemeshos/go-spacemesh/miner/mocks"
	"github.com/spacemeshos/go-spacemesh/sql"
	"github.com/spacemeshos/go-spacemesh/sql/atxs"
	"github.com/spacemeshos/go-spacemesh/sql/ballots"
	"github.com/spacemeshos/go-spacemesh/sql/blocks"
	"github.com/spacemeshos/go-spacemesh/sql/identities"
	"github.com/spacemeshos/go-spacemesh/sql/localsql"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
)

type expect struct {
	set    []types.ATXID
	id     types.Hash32
	weight uint64
}

func expectSet(set []types.ATXID, weight uint64) *expect {
	return &expect{
		id:     types.ATXIDList(set).Hash(),
		set:    set,
		weight: weight,
	}
}

type test struct {
	desc       string
	atxs       []*types.VerifiedActivationTx
	malfeasent []identity
	blocks     []*types.Block
	ballots    []*types.Ballot
	fallbacks  []types.EpochActiveSet

	networkDelay time.Duration
	current      types.LayerID
	target       types.EpochID

	expect    *expect
	expectErr string
}

func TestActiveSetGenerate(t *testing.T) {
	for _, tc := range []test{
		{
			desc: "fallback success",
			atxs: []*types.VerifiedActivationTx{
				gatx(types.ATXID{1}, 2, types.NodeID{1}, 2, genAtxWithReceived(time.Unix(20, 0))),
				gatx(types.ATXID{2}, 2, types.NodeID{1}, 3, genAtxWithReceived(time.Unix(20, 0))),
			},
			fallbacks: []types.EpochActiveSet{
				{
					Epoch: 3,
					Set: []types.ATXID{
						types.ATXID{1},
						types.ATXID{2},
					},
				},
			},
			target: 3,
			expect: expectSet([]types.ATXID{types.ATXID{1}, types.ATXID{2}}, 5*ticks),
		},
		{
			desc: "fallback failure",
			atxs: []*types.VerifiedActivationTx{
				gatx(types.ATXID{1}, 2, types.NodeID{1}, 2, genAtxWithReceived(time.Unix(20, 0))),
			},
			fallbacks: []types.EpochActiveSet{
				{
					Epoch: 3,
					Set: []types.ATXID{
						types.ATXID{1},
						types.ATXID{2},
					},
				},
			},
			target:    3,
			expectErr: "atx 3/0200000000 is missing in atxsdata",
		},
	} {
		tc := tc
		t.Run(tc.desc, func(t *testing.T) {
			t.Parallel()

			var (
				db       = sql.InMemory()
				localdb  = localsql.InMemory()
				atxsdata = atxsdata.New()
				ctrl     = gomock.NewController(t)
				clock    = mocks.NewMocklayerClock(ctrl)
				cfg      = config{networkDelay: tc.networkDelay}
				gen      = newActiveSetGenerator(cfg, logtest.New(t).Zap(), db, localdb, atxsdata, clock)
			)
			for _, atx := range tc.atxs {
				require.NoError(t, atxs.Add(db, atx))
				atxsdata.AddFromHeader(atx.ToHeader(), types.VRFPostIndex(0), false)
			}
			for _, identity := range tc.malfeasent {
				require.NoError(
					t,
					identities.SetMalicious(db, identity.id, codec.MustEncode(&identity.proof), identity.received),
				)
			}
			for _, block := range tc.blocks {
				require.NoError(t, blocks.Add(db, block))
			}
			for _, ballot := range tc.ballots {
				require.NoError(t, ballots.Add(db, ballot))
			}
			for _, fallback := range tc.fallbacks {
				gen.updateFallback(fallback.Epoch, fallback.Set)
			}

			id, setWeight, set, err := gen.generate(tc.current, tc.target)
			if tc.expectErr != "" {
				require.ErrorContains(t, err, tc.expectErr)
			} else {
				require.NoError(t, err)
			}
			if tc.expect != nil {
				require.Equal(t, tc.expect.id, id)
				require.Equal(t, tc.expect.weight, setWeight)
				require.Equal(t, tc.expect.set, set)
			}
		})
	}
}
