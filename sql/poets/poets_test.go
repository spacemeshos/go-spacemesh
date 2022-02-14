package poets

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/spacemeshos/go-spacemesh/sql"
)

func TestGetPoET(t *testing.T) {
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

	for i, proof := range proofs {
		require.NoError(t, AddPoET(db, refs[i], proof))
	}

	for i := range proofs {
		proof, err := GetPoET(db, refs[i])
		require.NoError(t, err)
		require.Equal(t, proofs[i], proof)
	}

	_, err := GetPoET(db, []byte("ref0"))
	require.ErrorIs(t, err, sql.ErrNotFound)
}

func TestAddPoET(t *testing.T) {
	db := sql.InMemory()

	ref := []byte("ref0")
	poet := []byte("proof0")

	_, err := GetPoET(db, ref)
	require.ErrorIs(t, err, sql.ErrNotFound)

	require.NoError(t, AddPoET(db, ref, poet))
	require.ErrorIs(t, AddPoET(db, ref, poet), sql.ErrObjectExists)

	got, err := GetPoET(db, ref)
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

	for i, ref := range refs {
		require.NoError(t, AddRef(db, sids[i], rids[i], ref))
	}

	for i := range refs {
		ref, err := GetRef(db, sids[i], rids[i])
		require.NoError(t, err)
		require.Equal(t, refs[i], ref)
	}

	_, err := GetRef(db, []byte("sid0"), "rid0")
	require.ErrorIs(t, err, sql.ErrNotFound)
}

func TestAddRef(t *testing.T) {
	db := sql.InMemory()

	sid := []byte("sid0")
	rid := "rid0"
	ref := []byte("ref0")

	_, err := GetRef(db, sid, rid)
	require.ErrorIs(t, err, sql.ErrNotFound)

	require.NoError(t, AddRef(db, sid, rid, ref))
	require.ErrorIs(t, AddRef(db, sid, rid, ref), sql.ErrObjectExists)

	got, err := GetRef(db, sid, rid)
	require.NoError(t, err)
	require.Equal(t, ref, got)
}

func TestDeleteRef(t *testing.T) {
	db := sql.InMemory()

	sid := []byte("sid0")
	rid := "rid0"
	ref := []byte("ref0")

	require.NoError(t, AddRef(db, sid, rid, ref))

	got, err := GetRef(db, sid, rid)
	require.NoError(t, err)
	require.Equal(t, ref, got)

	err = DeleteRef(db, sid, rid)
	require.NoError(t, err)

	_, err = GetRef(db, sid, rid)
	require.ErrorIs(t, err, sql.ErrNotFound)
}
