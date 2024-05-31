package poets

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/sql"
	"github.com/spacemeshos/go-spacemesh/sql/statesql"
)

func TestHas(t *testing.T) {
	db := statesql.InMemory()

	refs := []types.PoetProofRef{
		{0xca, 0xfe},
		{0xde, 0xad},
	}

	proofs := [][]byte{
		[]byte("proof1"),
		[]byte("proof2"),
	}

	sids := [][]byte{
		[]byte("sid1"),
		[]byte("sid2"),
	}

	rids := []string{
		"rid1",
		"rid2",
	}

	for _, ref := range refs {
		exists, err := Has(db, ref)
		require.NoError(t, err)
		require.False(t, exists)
	}

	require.NoError(t, Add(db, refs[0], proofs[0], sids[0], rids[0]))

	exists, err := Has(db, refs[0])
	require.NoError(t, err)
	assert.True(t, exists)

	exists, err = Has(db, refs[1])
	require.NoError(t, err)
	assert.False(t, exists)
}

func TestGet(t *testing.T) {
	db := statesql.InMemory()

	refs := []types.PoetProofRef{
		{0xca, 0xfe},
		{0xde, 0xad},
		{0xbe, 0xef},
	}

	proofs := [][]byte{
		[]byte("proof1"),
		[]byte("proof2"),
		[]byte("proof3"),
	}

	sids := [][]byte{
		[]byte("sid1"),
		[]byte("sid2"),
		[]byte("sid3"),
	}

	rids := []string{
		"rid1",
		"rid2",
		"rid3",
	}

	for i, proof := range proofs {
		require.NoError(t, Add(db, refs[i], proof, sids[i], rids[i]))
	}

	for i := range proofs {
		proof, err := Get(db, refs[i])
		require.NoError(t, err)
		require.Equal(t, proofs[i], proof)
	}

	noSuchRef := types.PoetProofRef{0xab, 0xba}
	_, err := Get(db, noSuchRef)
	require.ErrorIs(t, err, sql.ErrNotFound)

	sizes, err := GetBlobSizes(db, [][]byte{refs[0][:], refs[1][:], refs[2][:], noSuchRef[:]})
	require.NoError(t, err)
	require.Equal(t, []int{
		len(proofs[0]),
		len(proofs[1]),
		len(proofs[2]),
		-1,
	}, sizes)
}

func TestAdd(t *testing.T) {
	db := statesql.InMemory()

	ref := types.PoetProofRef{0xca, 0xfe}
	poet := []byte("proof0")
	sid := []byte("sid0")
	rid := "rid0"

	_, err := Get(db, ref)
	require.ErrorIs(t, err, sql.ErrNotFound)

	require.NoError(t, Add(db, ref, poet, sid, rid))
	require.ErrorIs(t, Add(db, ref, poet, sid, rid), sql.ErrObjectExists)

	got, err := Get(db, ref)
	require.NoError(t, err)
	require.Equal(t, poet, got)
}

func TestGetRef(t *testing.T) {
	db := statesql.InMemory()

	sids := [][]byte{
		[]byte("sid1"),
		[]byte("sid2"),
		[]byte("sid3"),
	}

	rids := []string{
		"rid1",
		"rid2",
		"rid3",
	}

	refs := []types.PoetProofRef{
		{0xca, 0xfe},
		{0xde, 0xad},
		{0xbe, 0xef},
	}

	proofs := [][]byte{
		[]byte("proof1"),
		[]byte("proof2"),
		[]byte("proof3"),
	}

	for i, ref := range refs {
		require.NoError(t, Add(db, ref, proofs[i], sids[i], rids[i]))
	}

	for i := range refs {
		ref, err := GetRef(db, sids[i], rids[i])
		require.NoError(t, err)
		require.Equal(t, refs[i], ref)
	}

	_, err := GetRef(db, []byte("sid0"), "rid0")
	require.ErrorIs(t, err, sql.ErrNotFound)
}
