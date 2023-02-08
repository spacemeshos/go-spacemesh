package activation

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	poetHash "github.com/spacemeshos/poet/hash"
	"github.com/spacemeshos/poet/prover"
	"github.com/spacemeshos/poet/shared"
	"github.com/spacemeshos/poet/verifier"
	"github.com/stretchr/testify/require"

	"github.com/spacemeshos/go-spacemesh/codec"
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/hash"
	"github.com/spacemeshos/go-spacemesh/log/logtest"
	"github.com/spacemeshos/go-spacemesh/sql"
)

var (
	memberHash      = []byte{0x17, 0x51, 0xac, 0x12, 0xe7, 0xe, 0x15, 0xb4, 0xf7, 0x6c, 0x16, 0x77, 0x5c, 0xd3, 0x29, 0xae, 0x55, 0x97, 0x3b, 0x61, 0x25, 0x21, 0xda, 0xb2, 0xde, 0x82, 0x8a, 0x5c, 0xdb, 0x6c, 0x8a, 0xb3}
	proof           *types.PoetProofMessage
	createProofOnce sync.Once
)

func getPoetProof(t *testing.T) types.PoetProofMessage {
	createProofOnce.Do(func() {
		members := [][]byte{memberHash}
		challenge, err := prover.CalcTreeRoot(members)
		require.NoError(t, err)

		leaves, merkleProof, err := prover.GenerateProofWithoutPersistency(
			context.Background(),
			prover.TreeConfig{Datadir: t.TempDir()},
			poetHash.GenLabelHashFunc(challenge),
			poetHash.GenMerkleHashFunc(challenge),
			time.Now().Add(time.Millisecond*300),
			shared.T,
		)
		require.NoError(t, err)

		err = verifier.Validate(
			*merkleProof,
			poetHash.GenLabelHashFunc(challenge),
			poetHash.GenMerkleHashFunc(challenge),
			leaves,
			shared.T,
		)
		require.NoError(t, err)

		proof = &types.PoetProofMessage{
			PoetProof: types.PoetProof{
				MerkleProof: *merkleProof,
				Members:     members,
				LeafCount:   leaves,
			},
			PoetServiceID: []byte("poet_id_123456"),
			RoundID:       "1337",
		}
	})
	return *proof
}

func TestPoetDbHappyFlow(t *testing.T) {
	r := require.New(t)
	msg := getPoetProof(t)
	poetDb := NewPoetDb(sql.InMemory(), logtest.New(t))

	r.NoError(poetDb.Validate(msg.PoetProof, msg.PoetServiceID, msg.RoundID, nil))
	ref, err := msg.Ref()
	r.NoError(err)

	proofBytes, err := codec.Encode(&msg.PoetProof)
	r.NoError(err)
	expectedRef := hash.Sum(proofBytes)
	r.Equal(types.PoetProofRef(types.CalcHash32(expectedRef[:]).Bytes()), ref)

	r.NoError(poetDb.StoreProof(context.TODO(), ref, &msg))
	got, err := poetDb.GetProofRef(msg.PoetServiceID, msg.RoundID)
	r.NoError(err)
	r.Equal(ref, got)

	membership, err := poetDb.GetMembershipMap(ref)
	r.NoError(err)
	r.True(membership[types.BytesToHash(memberHash)])
	r.False(membership[types.BytesToHash([]byte("5"))])
}

func TestPoetDbPoetProofNoMembers(t *testing.T) {
	r := require.New(t)

	poetDb := NewPoetDb(sql.InMemory(), logtest.New(t))
	poetProof := getPoetProof(t)
	poetProof.Root = []byte("some other root")
	poetProof.Members = nil

	err := poetDb.Validate(poetProof.PoetProof, poetProof.PoetServiceID, poetProof.RoundID, nil)
	r.NoError(err)
}

func TestPoetDbInvalidPoetProof(t *testing.T) {
	r := require.New(t)
	msg := getPoetProof(t)
	poetDb := NewPoetDb(sql.InMemory(), logtest.New(t))
	msg.PoetProof.Root = []byte("some other root")

	err := poetDb.Validate(msg.PoetProof, msg.PoetServiceID, msg.RoundID, nil)
	r.EqualError(err, fmt.Sprintf("failed to validate poet proof for poetID %x round 1337: validate PoET: merkle proof not valid",
		msg.PoetServiceID[:5]))
	r.False(types.IsProcessingError(err))
}

func TestPoetDbNonExistingKeys(t *testing.T) {
	r := require.New(t)
	msg := getPoetProof(t)
	poetDb := NewPoetDb(sql.InMemory(), logtest.New(t))

	_, err := poetDb.GetProofRef(msg.PoetServiceID, "0")
	r.EqualError(err, fmt.Sprintf("could not fetch poet proof for poet ID %x in round %v: get value: database: not found", msg.PoetServiceID[:5], "0"))

	ref := []byte("abcde")
	_, err = poetDb.GetMembershipMap(ref)
	r.EqualError(err, fmt.Sprintf("could not fetch poet proof for ref %x: get proof from store: get value: database: not found", ref[:5]))
}
