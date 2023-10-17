package util

import (
	"os"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/sql"
	"github.com/spacemeshos/go-spacemesh/sql/activesets"
	"github.com/spacemeshos/go-spacemesh/sql/ballots"
)

func TestMain(m *testing.M) {
	types.SetLayersPerEpoch(4)
	res := m.Run()
	os.Exit(res)
}

func TestExtractActiveSet(t *testing.T) {
	db := sql.InMemory()
	current := types.LayerID(20)
	blts := make([]*types.Ballot, 0, current)
	hashes := []types.Hash32{types.RandomHash(), types.RandomHash()}
	actives := [][]types.ATXID{types.RandomActiveSet(11), types.RandomActiveSet(19)}
	for lid := types.EpochID(2).FirstLayer(); lid < current; lid++ {
		blt := types.NewExistingBallot(types.RandomBallotID(), types.RandomEdSignature(), types.NodeID{1}, lid)
		if lid%3 == 0 {
			blt.EpochData = &types.EpochData{
				ActiveSetHash: hashes[0],
			}
			blt.ActiveSet = actives[0]
		}
		if lid%3 == 1 {
			blt.EpochData = &types.EpochData{
				ActiveSetHash: hashes[1],
			}
			blt.ActiveSet = actives[1]
		}
		require.NoError(t, ballots.Add(db, &blt))
		blts = append(blts, &blt)
	}
	require.NoError(t, ExtractActiveSet(db))
	for _, b := range blts {
		got, err := ballots.Get(db, b.ID())
		require.NoError(t, err)
		require.Nil(t, got.ActiveSet)
	}
	for i, h := range hashes {
		got, err := activesets.Get(db, h)
		require.NoError(t, err)
		require.Equal(t, actives[i], got.Set)
	}
}
