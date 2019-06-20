package activation

import (
	"fmt"
	"github.com/nullstyle/go-xdr/xdr3"
	"github.com/spacemeshos/go-spacemesh/common"
	"github.com/spacemeshos/go-spacemesh/database"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/types"
	"github.com/spacemeshos/sha256-simd"
	"github.com/stretchr/testify/require"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestPoetDbHappyFlow(t *testing.T) {
	r := require.New(t)

	poetDb := NewPoetDb(database.NewMemDatabase(), log.NewDefault("poetdb_test"))

	file, err := os.Open(filepath.Join("test_resources", "poet.proof"))
	r.NoError(err)

	var poetProof types.PoetProof
	_, err = xdr.Unmarshal(file, &poetProof)
	r.NoError(err)
	r.EqualValues([][]byte{[]byte("1"), []byte("2"), []byte("3")}, poetProof.Members)
	var poetId [types.PoetIdLength]byte
	copy(poetId[:], "poet id")
	roundId := uint64(1337)

	processingIssue, err := poetDb.ValidateAndStorePoetProof(poetProof, poetId, roundId, nil)
	r.NoError(err)
	r.False(processingIssue)

	ref, err := poetDb.getPoetProofRef(makeKey(poetId, roundId))
	r.NoError(err)

	proofBytes, err := types.InterfaceToBytes(poetProof)
	r.NoError(err)
	expectedRef := sha256.Sum256(proofBytes)
	r.Equal(expectedRef[:], ref)

	membership, err := poetDb.GetMembershipByPoetProofRef(ref)
	r.NoError(err)
	r.True(membership[common.BytesToHash([]byte("1"))])
	r.False(membership[common.BytesToHash([]byte("5"))])
}

func TestPoetDbInvalidPoetProof(t *testing.T) {
	r := require.New(t)

	poetDb := NewPoetDb(database.NewMemDatabase(), log.NewDefault("poetdb_test"))

	file, err := os.Open(filepath.Join("test_resources", "poet.proof"))
	r.NoError(err)

	var poetProof types.PoetProof
	_, err = xdr.Unmarshal(file, &poetProof)
	r.NoError(err)
	r.EqualValues([][]byte{[]byte("1"), []byte("2"), []byte("3")}, poetProof.Members)
	var poetId [types.PoetIdLength]byte
	copy(poetId[:], "poet id")
	roundId := uint64(1337)
	poetProof.Root = []byte("some other root")

	processingIssue, err := poetDb.ValidateAndStorePoetProof(poetProof, poetId, roundId, nil)
	r.EqualError(err, fmt.Sprintf("failed to validate poet proof for poetId %x round 1337: merkle proof not valid",
		poetId))
	r.False(processingIssue)
}

func TestPoetDbNonExistingKeys(t *testing.T) {
	r := require.New(t)

	poetDb := NewPoetDb(database.NewMemDatabase(), log.NewDefault("poetdb_test"))

	var poetId [types.PoetIdLength]byte
	copy(poetId[:], "abc")

	key := makeKey(poetId, 0)
	_, err := poetDb.getPoetProofRef(key)
	r.EqualError(err, fmt.Sprintf("could not fetch poet proof for key %x: not found", key))

	_, err = poetDb.GetMembershipByPoetProofRef([]byte("abc"))
	r.EqualError(err, "could not fetch poet proof for ref 616263: not found")
}

func TestPoetDb_SubscribeToPoetProofRef(t *testing.T) {
	r := require.New(t)

	poetDb := NewPoetDb(database.NewMemDatabase(), log.NewDefault("poetdb_test"))

	var poetId [types.PoetIdLength]byte
	copy(poetId[:], "abc")

	ch := poetDb.SubscribeToPoetProofRef(poetId, 0)

	select {
	case <-ch:
		r.Fail("where did this come from?!")
	case <-time.After(2 * time.Millisecond):
	}

	file, err := os.Open(filepath.Join("test_resources", "poet.proof"))
	r.NoError(err)

	var poetProof types.PoetProof
	_, err = xdr.Unmarshal(file, &poetProof)
	r.NoError(err)

	processingIssue, err := poetDb.ValidateAndStorePoetProof(poetProof, poetId, 0, nil)
	r.NoError(err)
	r.False(processingIssue)

	select {
	case proofRef := <-ch:
		membership, err := poetDb.GetMembershipByPoetProofRef(proofRef)
		r.NoError(err)
		r.Equal(membershipSliceToMap(poetProof.Members), membership)
	case <-time.After(2 * time.Millisecond):
		r.Fail("timeout!")
	}
	_, ok := <-ch
	r.False(ok, "channel should be closed")

	newCh := poetDb.SubscribeToPoetProofRef(poetId, 0)

	select {
	case proofRef := <-newCh:
		membership, err := poetDb.GetMembershipByPoetProofRef(proofRef)
		r.NoError(err)
		r.Equal(membershipSliceToMap(poetProof.Members), membership)
	case <-time.After(2 * time.Millisecond):
		r.Fail("timeout!")
	}
	_, ok = <-newCh
	r.False(ok, "channel should be closed")
}
