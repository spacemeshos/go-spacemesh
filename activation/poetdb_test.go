package activation

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/spacemeshos/go-spacemesh/codec"
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/hash"
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

	proofBytes, err := codec.Encode(&msg.PoetProof)
	require.NoError(t, err)
	expectedRef := hash.Sum(proofBytes)
	require.Equal(t, types.PoetProofRef(types.CalcHash32(expectedRef[:]).Bytes()), ref)

	require.NoError(t, poetDb.StoreProof(context.TODO(), ref, msg))
	got, err := poetDb.GetProofRef(msg.PoetServiceID, msg.RoundID)
	require.NoError(t, err)
	assert.Equal(t, ref, got)

	membership, err := poetDb.GetMembershipMap(ref)
	require.NoError(t, err)
	assert.True(t, membership[types.BytesToHash([]byte("1"))])
	assert.False(t, membership[types.BytesToHash([]byte("5"))])
}

func TestPoetDbPoetProofNoMembers(t *testing.T) {
	r := require.New(t)

	poetDb := NewPoetDb(sql.InMemory(), logtest.New(t))

	file, err := os.Open(filepath.Join("test_resources", "poet.proof"))
	r.NoError(err)

	var poetProof types.PoetProof
	_, err = codec.DecodeFrom(file, &poetProof)
	r.NoError(err)
	r.EqualValues([][]byte{[]byte("1"), []byte("2"), []byte("3")}, poetProof.Members)
	poetID := []byte("poet_id_123456")
	roundID := "1337"
	poetProof.Root = []byte("some other root")

	poetProof.Members = nil

	err = poetDb.Validate(poetProof, poetID, roundID, nil)
	r.NoError(err)
	r.False(types.IsProcessingError(err))
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

	_, err := poetDb.GetProofRef(msg.PoetServiceID, "0")
	require.EqualError(t, err, fmt.Sprintf("could not fetch poet proof for poet ID %x in round %v: get value: database: not found", msg.PoetServiceID[:5], "0"))

	ref := []byte("abcde")
	_, err = poetDb.GetMembershipMap(ref)
	require.EqualError(t, err, fmt.Sprintf("could not fetch poet proof for ref %x: get proof from store: get value: database: not found", ref[:5]))
}
