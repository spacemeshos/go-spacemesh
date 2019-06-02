package activation

import (
	"bytes"
	"fmt"
	xdr "github.com/nullstyle/go-xdr/xdr3"
	"github.com/spacemeshos/go-spacemesh/common"
	"github.com/spacemeshos/go-spacemesh/database"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/types"
	"github.com/spacemeshos/sha256-simd"
	"github.com/stretchr/testify/require"
	"os"
	"path/filepath"
	"testing"
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

	err = poetDb.ValidateAndStorePoetProof(poetProof, poetId, roundId, nil)
	r.NoError(err)

	ref, err := poetDb.GetPoetProofRef(poetId, roundId)
	r.NoError(err)

	var proofBuf bytes.Buffer
	_, err = xdr.Marshal(&proofBuf, poetProof)
	r.NoError(err)
	expectedRef := sha256.Sum256(proofBuf.Bytes())
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

	err = poetDb.ValidateAndStorePoetProof(poetProof, poetId, roundId, nil)
	r.EqualError(err, fmt.Sprintf("failed to validate poet proof for poetId %x round 1337: merkle proof not valid",
		poetId))
}

func TestPoetDbNonExistingKeys(t *testing.T) {
	r := require.New(t)

	poetDb := NewPoetDb(database.NewMemDatabase(), log.NewDefault("poetdb_test"))

	var poetId [types.PoetIdLength]byte
	copy(poetId[:], "abc")

	_, err := poetDb.GetPoetProofRef(poetId, 0)
	r.EqualError(err, fmt.Sprintf("could not fetch poet proof for poetId %x round 0: not found", poetId))

	_, err = poetDb.GetMembershipByPoetProofRef([]byte("abc"))
	r.EqualError(err, "could not fetch poet proof for ref 616263: not found")
}
