package poets

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/spacemeshos/go-spacemesh/sql"
)

func TestHas(t *testing.T) {
	db := sql.InMemory()

	refs := [][]byte{
		[]byte("ref1"),
		[]byte("ref2"),
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
	db := sql.InMemory()

	refs := [][]byte{
		[]byte("ref1"),
		[]byte("ref2"),
		[]byte("ref3"),
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

	_, err := Get(db, []byte("ref0"))
	require.ErrorIs(t, err, sql.ErrNotFound)
}

func TestAdd(t *testing.T) {
	db := sql.InMemory()

	ref := []byte("ref0")
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
	db := sql.InMemory()

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

	refs := [][]byte{
		[]byte("ref1"),
		[]byte("ref2"),
		[]byte("ref3"),
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
