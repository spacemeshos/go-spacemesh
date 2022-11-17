package certificates

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/sql"
)

func makeCert(lid types.LayerID, bid types.BlockID) *types.Certificate {
	return &types.Certificate{
		BlockID: bid,
		Signatures: []types.CertifyMessage{
			{
				CertifyContent: types.CertifyContent{
					LayerID:        lid,
					BlockID:        bid,
					EligibilityCnt: 1,
					Proof:          []byte("not a fraud 1"),
				},
			},
			{
				CertifyContent: types.CertifyContent{
					LayerID:        lid,
					BlockID:        bid,
					EligibilityCnt: 2,
					Proof:          []byte("not a fraud 2"),
				},
			},
		},
	}
}

func TestCertificates(t *testing.T) {
	db := sql.InMemory()
	lid := types.NewLayerID(10)

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
	require.NoError(t, SetHareOutput(db, lid, bid2))
	got, err = Get(db, lid)
	require.NoError(t, err)
	require.Len(t, got, 2)
	require.Equal(t, bid1, got[0].Block)
	require.Equal(t, cert1, got[0].Cert)
	require.True(t, got[0].Valid)
	require.Equal(t, bid2, got[1].Block)
	require.Nil(t, got[1].Cert)
	require.True(t, got[1].Valid)
}

func TestHareOutput(t *testing.T) {
	db := sql.InMemory()
	lid := types.NewLayerID(10)

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
