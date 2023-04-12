package certificates

import (
	"os"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/sql"
)

const layersPerEpoch = 5

func TestMain(m *testing.M) {
	types.SetLayersPerEpoch(layersPerEpoch)

	res := m.Run()
	os.Exit(res)
}

func makeCert(lid types.LayerID, bid types.BlockID) *types.Certificate {
	return &types.Certificate{
		BlockID: bid,
		Signatures: []types.CertifyMessage{
			{
				CertifyContent: types.CertifyContent{
					LayerID:        lid,
					BlockID:        bid,
					EligibilityCnt: 1,
					Proof:          types.RandomVrfSignature(),
				},
			},
			{
				CertifyContent: types.CertifyContent{
					LayerID:        lid,
					BlockID:        bid,
					EligibilityCnt: 2,
					Proof:          types.RandomVrfSignature(),
				},
			},
		},
	}
}

func TestCertificates(t *testing.T) {
	db := sql.InMemory()
	lid := types.LayerID(10)

	got, err := Get(db, lid)
	require.ErrorIs(t, err, sql.ErrNotFound)
	require.Empty(t, got)

	bid1 := types.BlockID{1, 1, 1}
	require.NoError(t, SetHareOutput(db, lid, bid1))
	got, err = Get(db, lid)
	require.NoError(t, err)
	require.Len(t, got, 1)
	require.Equal(t, bid1, got[0].Block)
	require.Nil(t, got[0].Cert)
	require.True(t, got[0].Valid)

	cert1 := makeCert(lid, bid1)
	require.NoError(t, Add(db, lid, cert1))
	got, err = Get(db, lid)
	require.NoError(t, err)
	require.Len(t, got, 1)
	require.Equal(t, bid1, got[0].Block)
	require.Equal(t, cert1, got[0].Cert)
	require.True(t, got[0].Valid)

	require.NoError(t, SetInvalid(db, lid, bid1))
	got, err = Get(db, lid)
	require.NoError(t, err)
	require.Len(t, got, 1)
	require.False(t, got[0].Valid)
	require.NoError(t, SetValid(db, lid, bid1))
	got, err = Get(db, lid)
	require.NoError(t, err)
	require.Len(t, got, 1)
	require.True(t, got[0].Valid)

	bid2 := types.BlockID{2, 2, 2}
	require.NoError(t, SetHareOutputInvalid(db, lid, bid2))
	got, err = Get(db, lid)
	require.NoError(t, err)
	require.Len(t, got, 2)
	require.Equal(t, bid1, got[0].Block)
	require.Equal(t, cert1, got[0].Cert)
	require.True(t, got[0].Valid)
	require.Equal(t, bid2, got[1].Block)
	require.Nil(t, got[1].Cert)
	require.False(t, got[1].Valid)
}

func TestHareOutput(t *testing.T) {
	db := sql.InMemory()
	lid := types.LayerID(10)

	ho, err := GetHareOutput(db, lid)
	require.ErrorIs(t, err, sql.ErrNotFound)
	require.Equal(t, types.EmptyBlockID, ho)

	bid1 := types.BlockID{1, 1, 1}
	require.NoError(t, SetHareOutput(db, lid, bid1))
	ho, err = GetHareOutput(db, lid)
	require.NoError(t, err)
	require.Equal(t, bid1, ho)

	cert1 := makeCert(lid, bid1)
	require.NoError(t, Add(db, lid, cert1))
	ho, err = GetHareOutput(db, lid)
	require.NoError(t, err)
	require.Equal(t, bid1, ho)

	bid2 := types.BlockID{2, 2, 2}
	cert2 := makeCert(lid, bid2)
	require.NoError(t, Add(db, lid, cert2))
	ho, err = GetHareOutput(db, lid)
	require.NoError(t, err)
	require.Equal(t, types.EmptyBlockID, ho)

	require.NoError(t, SetInvalid(db, lid, bid1))
	ho, err = GetHareOutput(db, lid)
	require.NoError(t, err)
	require.Equal(t, bid2, ho)

	require.NoError(t, SetInvalid(db, lid, bid2))
	ho, err = GetHareOutput(db, lid)
	require.ErrorIs(t, err, sql.ErrNotFound)
	require.Equal(t, types.EmptyBlockID, ho)
}

func TestFirstInEpoch(t *testing.T) {
	db := sql.InMemory()

	lyrBlocks := map[types.LayerID]types.BlockID{
		types.LayerID(layersPerEpoch - 1):   {1}, // epoch 0
		types.LayerID(layersPerEpoch + 1):   {2}, // 2nd layer of epoch 1
		types.LayerID(layersPerEpoch + 2):   {3}, // 3d layer of epoch 1
		types.LayerID(2*layersPerEpoch - 1): {4}, // last layer of epoch 1
		// epoch 2 has no hare output
		types.LayerID(3 * layersPerEpoch): {5}, // first layer of epoch 3
	}
	for lid, bid := range lyrBlocks {
		require.NoError(t, SetHareOutput(db, lid, bid))
	}

	got, err := FirstInEpoch(db, types.EpochID(0))
	require.NoError(t, err)
	require.Equal(t, types.BlockID{1}, got)

	got, err = FirstInEpoch(db, types.EpochID(1))
	require.NoError(t, err)
	require.Equal(t, types.BlockID{2}, got)

	got, err = FirstInEpoch(db, types.EpochID(2))
	require.ErrorIs(t, err, sql.ErrNotFound)
	require.Equal(t, types.EmptyBlockID, got)

	got, err = FirstInEpoch(db, types.EpochID(3))
	require.NoError(t, err)
	require.Equal(t, types.BlockID{5}, got)
}
