package activation

import (
	"fmt"
	"github.com/nullstyle/go-xdr/xdr3"
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/database"
	"github.com/spacemeshos/go-spacemesh/log"
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
	poetID := []byte("poet_id_123456")
	roundID := "1337"

	err = poetDb.Validate(poetProof, poetID, roundID, nil)
	r.NoError(err)
	r.False(types.IsProcessingError(err))

	err = poetDb.storeProof(&types.PoetProofMessage{
		PoetProof:     poetProof,
		PoetServiceID: poetID,
		RoundID:       roundID,
		Signature:     nil,
	})
	r.NoError(err)

	ref, err := poetDb.getProofRef(makeKey(poetID, roundID))
	r.NoError(err)

	proofBytes, err := types.InterfaceToBytes(poetProof)
	r.NoError(err)
	expectedRef := sha256.Sum256(proofBytes)
	r.Equal(types.CalcHash32(expectedRef[:]).Bytes(), ref)

	membership, err := poetDb.GetMembershipMap(ref)
	r.NoError(err)
	r.True(membership[types.BytesToHash([]byte("1"))])
	r.False(membership[types.BytesToHash([]byte("5"))])
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
	poetID := []byte("poet_id_123456")
	roundID := "1337"
	poetProof.Root = []byte("some other root")

	err = poetDb.Validate(poetProof, poetID, roundID, nil)
	r.EqualError(err, fmt.Sprintf("failed to validate poet proof for poetID %x round 1337: merkle proof not valid",
		poetID[:5]))
	r.False(types.IsProcessingError(err))
}

func TestPoetDbNonExistingKeys(t *testing.T) {
	r := require.New(t)

	poetDb := NewPoetDb(database.NewMemDatabase(), log.NewDefault("poetdb_test"))

	poetID := []byte("poet_id_123456")

	key := makeKey(poetID, "0")
	_, err := poetDb.getProofRef(key)
	r.EqualError(err, fmt.Sprintf("could not fetch poet proof for key %x: leveldb: not found", key[:5]))

	ref := []byte("abcde")
	_, err = poetDb.GetMembershipMap(ref)
	r.EqualError(err, fmt.Sprintf("could not fetch poet proof for ref %x: leveldb: not found", ref[:3]))
}

func TestPoetDb_SubscribeToPoetProofRef(t *testing.T) {
	r := require.New(t)

	poetDb := NewPoetDb(database.NewMemDatabase(), log.NewDefault("poetdb_test"))

	poetID := []byte("poet_id_123456")

	ch := poetDb.SubscribeToProofRef(poetID, "0")

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

	err = poetDb.Validate(poetProof, poetID, "0", nil)
	r.NoError(err)
	r.False(types.IsProcessingError(err))

	err = poetDb.storeProof(&types.PoetProofMessage{
		PoetProof:     poetProof,
		PoetServiceID: poetID,
		RoundID:       "0",
		Signature:     nil,
	})
	r.NoError(err)

	select {
	case proofRef := <-ch:
		membership, err := poetDb.GetMembershipMap(proofRef)
		r.NoError(err)
		r.Equal(membershipSliceToMap(poetProof.Members), membership)
	case <-time.After(2 * time.Millisecond):
		r.Fail("timeout!")
	}
	_, ok := <-ch
	r.False(ok, "channel should be closed")

	newCh := poetDb.SubscribeToProofRef(poetID, "0")

	select {
	case proofRef := <-newCh:
		membership, err := poetDb.GetMembershipMap(proofRef)
		r.NoError(err)
		r.Equal(membershipSliceToMap(poetProof.Members), membership)
	case <-time.After(2 * time.Millisecond):
		r.Fail("timeout!")
	}
	_, ok = <-newCh
	r.False(ok, "channel should be closed")
}
