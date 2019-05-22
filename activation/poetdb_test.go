package activation

import (
	xdr "github.com/nullstyle/go-xdr/xdr3"
	"github.com/spacemeshos/go-spacemesh/common"
	"github.com/spacemeshos/go-spacemesh/database"
	"github.com/spacemeshos/go-spacemesh/types"
	"github.com/stretchr/testify/require"
	"os"
	"path/filepath"
	"testing"
)

func TestPoetDbHappyFlow(t *testing.T) {
	r := require.New(t)

	poetDb := NewPoetDb(database.NewMemDatabase())

	membershipProof := types.PoetMembershipProof{
		Members: [][]byte{[]byte("1"), []byte("2"), []byte("3")},
	}

	err := poetDb.AddMembershipProof(membershipProof)
	r.NoError(err)

	membershipRoot, err := calcRoot(membershipProof.Members)
	r.NoError(err)

	file, err := os.Open(filepath.Join("test_resources", "poet.proof"))
	r.NoError(err)

	var poetProof types.PoetProof
	_, err = xdr.Unmarshal(file, &poetProof)
	r.NoError(err)
	r.Equal(membershipRoot, poetProof.MembershipRoot)
	poetProof.PoetId = []byte("poet id")
	poetProof.RoundId = 1337

	err = poetDb.AddPoetProof(poetProof)
	r.NoError(err)

	root, err := poetDb.GetPoetProofRoot(poetProof.PoetId, &types.PoetRound{Id: poetProof.RoundId})
	r.NoError(err)
	r.Equal(poetProof.Root, root)

	membership, err := poetDb.GetMembershipByPoetProofRoot(root)
	r.NoError(err)
	r.True(membership[common.BytesToHash([]byte("1"))])
	r.False(membership[common.BytesToHash([]byte("5"))])
}

func TestPoetDbMissingMembershipProof(t *testing.T) {
	r := require.New(t)

	poetDb := NewPoetDb(database.NewMemDatabase())

	file, err := os.Open(filepath.Join("test_resources", "poet.proof"))
	r.NoError(err)

	var poetProof types.PoetProof
	_, err = xdr.Unmarshal(file, &poetProof)
	r.NoError(err)
	poetProof.PoetId = []byte("poet id")
	poetProof.RoundId = 1337

	err = poetDb.AddPoetProof(poetProof)
	r.EqualError(err, "failed to fetch matching membership proof: not found")
}

func TestPoetDbInvalidPoetProof(t *testing.T) {
	r := require.New(t)

	poetDb := NewPoetDb(database.NewMemDatabase())

	membershipProof := types.PoetMembershipProof{
		Members: [][]byte{[]byte("1"), []byte("2"), []byte("3")},
	}

	err := poetDb.AddMembershipProof(membershipProof)
	r.NoError(err)

	membershipRoot, err := calcRoot(membershipProof.Members)
	r.NoError(err)

	file, err := os.Open(filepath.Join("test_resources", "poet.proof"))
	r.NoError(err)

	var poetProof types.PoetProof
	_, err = xdr.Unmarshal(file, &poetProof)
	r.NoError(err)
	r.Equal(membershipRoot, poetProof.MembershipRoot)
	poetProof.PoetId = []byte("poet id")
	poetProof.RoundId = 1337
	poetProof.Root = []byte("some other root")

	err = poetDb.AddPoetProof(poetProof)
	r.EqualError(err, "failed to validate poet proof: merkle proof not valid")
}

func TestPoetDbNonExistingKeys(t *testing.T) {
	r := require.New(t)

	poetDb := NewPoetDb(database.NewMemDatabase())

	_, err := poetDb.GetPoetProofRoot(nil, &types.PoetRound{})
	r.EqualError(err, "not found")

	_, err = poetDb.GetMembershipByPoetProofRoot(nil)
	r.EqualError(err, "not found")
}