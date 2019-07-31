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
	var poetId [types.PoetServiceIdLength]byte
	copy(poetId[:], "poet id")
	roundId := uint64(1337)

	err = poetDb.Validate(poetProof, poetId, roundId, nil)
	r.NoError(err)
	r.False(types.IsProcessingError(err))

	err = poetDb.storeProof(&types.PoetProofMessage{
		PoetProof:     poetProof,
		PoetServiceId: poetId,
		RoundId:       roundId,
		Signature:     nil,
	})
	r.NoError(err)

	ref, err := poetDb.getProofRef(makeKey(poetId, roundId))
	r.NoError(err)

	proofBytes, err := types.InterfaceToBytes(poetProof)
	r.NoError(err)
	expectedRef := sha256.Sum256(proofBytes)
	r.Equal(expectedRef[:], ref)

	membership, err := poetDb.GetMembershipMap(ref)
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
	var poetId [types.PoetServiceIdLength]byte
	copy(poetId[:], "poet id")
	roundId := uint64(1337)
	poetProof.Root = []byte("some other root")

	err = poetDb.Validate(poetProof, poetId, roundId, nil)
	r.EqualError(err, fmt.Sprintf("failed to validate poet proof for poetId %x round 1337: merkle proof not valid",
		poetId[:5]))
	r.False(types.IsProcessingError(err))
}

func TestPoetDbNonExistingKeys(t *testing.T) {
	r := require.New(t)

	poetDb := NewPoetDb(database.NewMemDatabase(), log.NewDefault("poetdb_test"))

	var poetId [types.PoetServiceIdLength]byte
	copy(poetId[:], "abc")

	key := makeKey(poetId, 0)
	_, err := poetDb.getProofRef(key)
	r.EqualError(err, fmt.Sprintf("could not fetch poet proof for key %x: leveldb: not found", key[:5]))

	ref := []byte("abcde")
	_, err = poetDb.GetMembershipMap(ref)
	r.EqualError(err, fmt.Sprintf("could not fetch poet proof for ref %x: leveldb: not found", ref[:3]))
}

func TestPoetDb_SubscribeToPoetProofRef(t *testing.T) {
	r := require.New(t)

	poetDb := NewPoetDb(database.NewMemDatabase(), log.NewDefault("poetdb_test"))

	var poetId [types.PoetServiceIdLength]byte
	copy(poetId[:], "abc")

	ch := poetDb.SubscribeToProofRef(poetId, 0)

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

	err = poetDb.Validate(poetProof, poetId, 0, nil)
	r.NoError(err)
	r.False(types.IsProcessingError(err))

	err = poetDb.storeProof(&types.PoetProofMessage{
		PoetProof:     poetProof,
		PoetServiceId: poetId,
		RoundId:       0,
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

	newCh := poetDb.SubscribeToProofRef(poetId, 0)

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
