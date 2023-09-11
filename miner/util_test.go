package miner

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/datastore"
	"github.com/spacemeshos/go-spacemesh/log/logtest"
	"github.com/spacemeshos/go-spacemesh/signing"
	"github.com/spacemeshos/go-spacemesh/sql"
	"github.com/spacemeshos/go-spacemesh/sql/blocks"
	"github.com/spacemeshos/go-spacemesh/sql/certificates"
	"github.com/spacemeshos/go-spacemesh/sql/layers"
)

func TestActiveSetFromEpochFirstBlock(t *testing.T) {
	for _, tc := range []struct {
		desc               string
		certified, applied bool
		err                error
	}{
		{
			desc:      "with certified",
			certified: true,
		},
		{
			desc:    "w/o certified",
			applied: true,
		},
		{
			desc: "nothing",
			err:  sql.ErrNotFound,
		},
	} {
		t.Run(tc.desc, func(t *testing.T) {
			epoch := types.EpochID(3)
			cdb := datastore.NewCachedDB(sql.InMemory(), logtest.New(t))

			got, err := ActiveSetFromEpochFirstCertifiedBlock(cdb, epoch)
			require.ErrorIs(t, err, sql.ErrNotFound)
			require.Nil(t, got)

			var expected []types.ATXID
			for i := uint32(0); i < layersPerEpoch; i++ {
				lid := epoch.FirstLayer() + types.LayerID(i)
				all := types.RandomActiveSet(10)
				blts := createBallots(t, cdb, lid, 5, all)
				block := &types.Block{
					InnerBlock: types.InnerBlock{
						LayerIndex: lid,
					},
				}
				for _, b := range blts {
					block.Rewards = append(block.Rewards, types.AnyReward{AtxID: b.AtxID})
					all = append(all, b.AtxID)
				}
				block.Initialize()
				require.NoError(t, blocks.Add(cdb, block))
				if tc.certified {
					require.NoError(t, certificates.Add(cdb, lid, &types.Certificate{BlockID: block.ID()}))
				} else if tc.applied {
					require.NoError(t, layers.SetApplied(cdb, lid, block.ID()))
				}
				if i == 0 {
					expected = all
				}
				for _, id := range all {
					signer, err := signing.NewEdSigner()
					require.NoError(t, err)
					genMinerATX(t, cdb, id, (epoch - 1).FirstLayer(), signer, time.Now())
				}
			}

			got, err = ActiveSetFromEpochFirstBlock(cdb, epoch)
			if tc.err != nil {
				require.ErrorIs(t, err, tc.err)
			} else {
				require.NoError(t, err)
				require.ElementsMatch(t, expected, got)
			}
			if tc.certified {
				got, err = ActiveSetFromEpochFirstCertifiedBlock(cdb, epoch)
				require.NoError(t, err)
				require.ElementsMatch(t, expected, got)
			}
		})
	}
}
