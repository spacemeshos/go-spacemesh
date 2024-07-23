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
	"go.uber.org/zap/zaptest"

	"github.com/spacemeshos/go-spacemesh/codec"
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/sql"
)

var (
	proof           *types.PoetProofMessage
	createProofOnce sync.Once
)

func getPoetProof(t *testing.T) types.PoetProofMessage {
	createProofOnce.Do(func() {
		challenge := []byte("hello world, this is a challenge")

		leaves, merkleProof, err := prover.GenerateProofWithoutPersistency(
			context.Background(),
			prover.TreeConfig{Datadir: t.TempDir()},
			poetHash.GenLabelHashFunc(challenge),
			poetHash.GenMerkleHashFunc(challenge),
			time.Now(),
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
				LeafCount:   leaves,
			},
			PoetServiceID: []byte("poet_id_123456"),
			RoundID:       "1337",
			Statement:     types.BytesToHash(challenge),
		}
	})
	return *proof
}

func TestPoetDbHappyFlow(t *testing.T) {
	r := require.New(t)
	msg := getPoetProof(t)
	poetDb := NewPoetDb(sql.InMemory(), zaptest.NewLogger(t))

	r.NoError(poetDb.Validate(msg.Statement[:], msg.PoetProof, msg.PoetServiceID, msg.RoundID, types.EmptyEdSignature))
	ref, err := msg.Ref()
	r.NoError(err)

	proofBytes, err := codec.Encode(&msg.PoetProof)
	r.NoError(err)
	expectedRef := types.CalcHash32(proofBytes)
	r.Equal(types.PoetProofRef(expectedRef), ref)

	r.NoError(poetDb.StoreProof(context.Background(), ref, &msg))
	got, err := poetDb.GetProofRef(msg.PoetServiceID, msg.RoundID)
	r.NoError(err)
	r.Equal(ref, got)
}

func TestPoetDbInvalidPoetProof(t *testing.T) {
	r := require.New(t)
	msg := getPoetProof(t)
	poetDb := NewPoetDb(sql.InMemory(), zaptest.NewLogger(t))
	msg.PoetProof.Root = []byte("some other root")

	err := poetDb.Validate(msg.Statement[:], msg.PoetProof, msg.PoetServiceID, msg.RoundID, types.EmptyEdSignature)
	r.EqualError(
		err,
		fmt.Sprintf(
			"failed to validate poet proof for poetID %x round 1337: validate PoET: merkle proof not valid",
			msg.PoetServiceID[:5],
		),
	)
}

func TestPoetDbInvalidPoetStatement(t *testing.T) {
	r := require.New(t)
	msg := getPoetProof(t)
	poetDb := NewPoetDb(sql.InMemory(), zaptest.NewLogger(t))
	msg.Statement = types.CalcHash32([]byte("some other statement"))

	err := poetDb.Validate(msg.Statement[:], msg.PoetProof, msg.PoetServiceID, msg.RoundID, types.EmptyEdSignature)
	r.EqualError(
		err,
		fmt.Sprintf(
			"failed to validate poet proof for poetID %x round 1337: validate PoET: merkle proof not valid",
			msg.PoetServiceID[:5],
		),
	)
}

func TestPoetDbNonExistingKeys(t *testing.T) {
	r := require.New(t)
	msg := getPoetProof(t)
	poetDb := NewPoetDb(sql.InMemory(), zaptest.NewLogger(t))

	_, err := poetDb.GetProofRef(msg.PoetServiceID, "0")
	r.EqualError(
		err,
		fmt.Sprintf(
			"could not fetch poet proof for poet ID %x in round %v: get value: database: not found",
			msg.PoetServiceID[:5],
			"0",
		),
	)
}
