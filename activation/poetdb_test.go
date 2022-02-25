package activation

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/spacemeshos/sha256-simd"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/spacemeshos/go-spacemesh/codec"
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/log/logtest"
	"github.com/spacemeshos/go-spacemesh/sql"
)

func readPoetProofFromDisk(t *testing.T) *types.PoetProofMessage {
	file, err := os.Open(filepath.Join("test_resources", "poet.proof"))
	require.NoError(t, err)

	var poetProof types.PoetProof
	_, err = codec.DecodeFrom(file, &poetProof)
	require.NoError(t, err)
	require.EqualValues(t, [][]byte{[]byte("1"), []byte("2"), []byte("3")}, poetProof.Members)
	poetID := []byte("poet_id_123456")
	roundID := "1337"
	return &types.PoetProofMessage{
		PoetProof:     poetProof,
		PoetServiceID: poetID,
		RoundID:       roundID,
		Signature:     nil,
	}
}

func TestPoetDbHappyFlow(t *testing.T) {
	msg := readPoetProofFromDisk(t)
	poetDb := NewPoetDb(sql.InMemory(), logtest.New(t))

	require.NoError(t, poetDb.Validate(msg.PoetProof, msg.PoetServiceID, msg.RoundID, nil))
	ref, err := msg.Ref()
	require.NoError(t, err)

	proofBytes, err := types.InterfaceToBytes(msg.PoetProof)
	require.NoError(t, err)
	expectedRef := sha256.Sum256(proofBytes)
	require.Equal(t, types.CalcHash32(expectedRef[:]).Bytes(), ref)

	require.NoError(t, poetDb.StoreProof(ref, msg))
	got, err := poetDb.getProofRef(msg.PoetServiceID, msg.RoundID)
	require.NoError(t, err)
	assert.Equal(t, ref, got)

	membership, err := poetDb.GetMembershipMap(ref)
	require.NoError(t, err)
	assert.True(t, membership[types.BytesToHash([]byte("1"))])
	assert.False(t, membership[types.BytesToHash([]byte("5"))])
}

func TestPoetDbInvalidPoetProof(t *testing.T) {
	msg := readPoetProofFromDisk(t)
	poetDb := NewPoetDb(sql.InMemory(), logtest.New(t))
	msg.PoetProof.Root = []byte("some other root")

	err := poetDb.Validate(msg.PoetProof, msg.PoetServiceID, msg.RoundID, nil)
	require.EqualError(t, err, fmt.Sprintf("failed to validate poet proof for poetID %x round 1337: validate PoET: merkle proof not valid",
		msg.PoetServiceID[:5]))
	assert.False(t, types.IsProcessingError(err))
}

func TestPoetDbNonExistingKeys(t *testing.T) {
	msg := readPoetProofFromDisk(t)
	poetDb := NewPoetDb(sql.InMemory(), logtest.New(t))

	_, err := poetDb.getProofRef(msg.PoetServiceID, "0")
	require.EqualError(t, err, fmt.Sprintf("could not fetch poet proof for poet ID %x in round %v: get value: leveldb: not found", msg.PoetServiceID[:5], "0"))

	ref := []byte("abcde")
	_, err = poetDb.GetMembershipMap(ref)
	require.EqualError(t, err, fmt.Sprintf("could not fetch poet proof for ref %x: get proof from store: get value: leveldb: not found", ref[:5]))
}

func TestPoetDb_SubscribeToPoetProofRef(t *testing.T) {
	msg := readPoetProofFromDisk(t)
	poetDb := NewPoetDb(sql.InMemory(), logtest.New(t))

	ch := poetDb.SubscribeToProofRef(msg.PoetServiceID, msg.RoundID)

	select {
	case <-ch:
		require.Fail(t, "where did this come from?!")
	case <-time.After(2 * time.Millisecond):
	}

	file, err := os.Open(filepath.Join("test_resources", "poet.proof"))
	require.NoError(t, err)

	var poetProof types.PoetProof
	_, err = codec.DecodeFrom(file, &poetProof)
	require.NoError(t, err)

	err = poetDb.Validate(poetProof, msg.PoetServiceID, msg.RoundID, nil)
	require.NoError(t, err)

	ref, err := msg.Ref()
	require.NoError(t, err)

	err = poetDb.StoreProof(ref, msg)
	require.NoError(t, err)

	select {
	case proofRef := <-ch:
		membership, err := poetDb.GetMembershipMap(proofRef)
		require.NoError(t, err)
		require.Equal(t, membershipSliceToMap(poetProof.Members), membership)
	case <-time.After(2 * time.Millisecond):
		require.Fail(t, "timeout!")
	}
	_, ok := <-ch
	require.False(t, ok, "channel should be closed")

	newCh := poetDb.SubscribeToProofRef(msg.PoetServiceID, msg.RoundID)

	select {
	case proofRef := <-newCh:
		membership, err := poetDb.GetMembershipMap(proofRef)
		require.NoError(t, err)
		require.Equal(t, membershipSliceToMap(poetProof.Members), membership)
	case <-time.After(2 * time.Millisecond):
		require.Fail(t, "timeout!")
	}
	_, ok = <-newCh
	require.False(t, ok, "channel should be closed")
}
