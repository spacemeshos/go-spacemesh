package proposals

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/spacemeshos/go-spacemesh/codec"
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/sql"
	"github.com/spacemeshos/go-spacemesh/sql/ballots"
	"github.com/spacemeshos/go-spacemesh/sql/identities"
)

func TestAdd(t *testing.T) {
	db := sql.InMemory()
	nodeID := types.RandomNodeID()
	ballot := types.NewExistingBallot(types.BallotID{1}, types.RandomEdSignature(), nodeID, types.LayerID(0))

	require.NoError(t, ballots.Add(db, &ballot))
	require.NoError(t, identities.SetMalicious(db, nodeID, []byte("proof"), time.Now()))
	proposal := &types.Proposal{
		InnerProposal: types.InnerProposal{
			Ballot:   ballot,
			TxIDs:    []types.TransactionID{{3, 4}},
			MeshHash: types.RandomHash(),
		},
		Signature: types.RandomEdSignature(),
	}
	proposal.SetID(types.ProposalID{7, 8})

	require.NoError(t, Add(db, proposal))
	require.ErrorIs(t, Add(db, proposal), sql.ErrObjectExists)
}

func TestDelete(t *testing.T) {
	db := sql.InMemory()
	nodeID := types.RandomNodeID()
	maxLayers := 10
	numProps := 3
	for i := 1; i <= maxLayers; i++ {
		for j := 0; j < numProps; j++ {
			ballot := types.NewExistingBallot(
				types.RandomBallotID(),
				types.RandomEdSignature(),
				nodeID,
				types.LayerID(i),
			)
			require.NoError(t, ballots.Add(db, &ballot))
			proposal := &types.Proposal{
				InnerProposal: types.InnerProposal{
					Ballot:   ballot,
					TxIDs:    []types.TransactionID{{3, 4}},
					MeshHash: types.RandomHash(),
				},
				Signature: types.RandomEdSignature(),
			}
			proposal.SetID(types.RandomProposalID())
			require.NoError(t, Add(db, proposal))
		}
		got, err := GetByLayer(db, types.LayerID(i))
		require.NoError(t, err)
		require.Len(t, got, numProps)
	}
	require.NoError(t, DeleteBefore(db, types.LayerID(maxLayers)))
	for i := 1; i < maxLayers; i++ {
		_, err := GetByLayer(db, types.LayerID(i))
		require.ErrorIs(t, err, sql.ErrNotFound)
	}
	got, err := GetByLayer(db, types.LayerID(maxLayers))
	require.NoError(t, err)
	require.Len(t, got, numProps)
}

func TestHas(t *testing.T) {
	db := sql.InMemory()
	nodeID := types.RandomNodeID()
	ballot := types.NewExistingBallot(types.BallotID{1}, types.RandomEdSignature(), nodeID, types.LayerID(0))

	require.NoError(t, ballots.Add(db, &ballot))
	require.NoError(t, identities.SetMalicious(db, nodeID, []byte("proof"), time.Now()))
	proposal := &types.Proposal{
		InnerProposal: types.InnerProposal{
			Ballot:   ballot,
			TxIDs:    []types.TransactionID{{3, 4}},
			MeshHash: types.RandomHash(),
		},
		Signature: types.RandomEdSignature(),
	}

	proposal.SetID(types.ProposalID{7, 8})

	require.NoError(t, Add(db, proposal))
	has, err := Has(db, proposal.ID())
	require.NoError(t, err)
	require.True(t, has)
}

func TestGet(t *testing.T) {
	db := sql.InMemory()

	nodeID := types.RandomNodeID()
	ballot := types.NewExistingBallot(types.BallotID{1}, types.RandomEdSignature(), nodeID, types.LayerID(0))

	require.NoError(t, ballots.Add(db, &ballot))
	require.NoError(t, identities.SetMalicious(db, nodeID, []byte("proof"), time.Now()))
	ballot.SetMalicious()

	proposal := &types.Proposal{
		InnerProposal: types.InnerProposal{
			Ballot:   ballot,
			TxIDs:    []types.TransactionID{{3, 4}},
			MeshHash: types.RandomHash(),
		},
		Signature: types.RandomEdSignature(),
	}

	proposal.SetID(types.ProposalID{7, 8})

	require.NoError(t, Add(db, proposal))

	got, err := Get(db, proposal.ID())
	require.NoError(t, err)
	require.EqualValues(t, proposal, got)
}

func TestLoadBlob(t *testing.T) {
	db := sql.InMemory()

	nodeID1 := types.RandomNodeID()
	ballot1 := types.NewExistingBallot(types.BallotID{1}, types.RandomEdSignature(), nodeID1, types.LayerID(0))
	require.NoError(t, ballots.Add(db, &ballot1))
	proposal1 := &types.Proposal{
		InnerProposal: types.InnerProposal{
			Ballot:   ballot1,
			TxIDs:    []types.TransactionID{{3, 4}},
			MeshHash: types.RandomHash(),
		},
		Signature: types.RandomEdSignature(),
	}
	proposal1.SetID(types.ProposalID{7, 8})
	require.NoError(t, Add(db, proposal1))

	var blob1 sql.Blob
	require.NoError(t, LoadBlob(db, proposal1.ID().Bytes(), &blob1))
	encoded, err := codec.Encode(proposal1)
	require.NoError(t, err)
	require.Equal(t, encoded, blob1.Bytes)
	blobSizes, err := GetBlobSizes(db, [][]byte{proposal1.ID().Bytes()})
	require.NoError(t, err)
	require.Equal(t, []int{len(blob1.Bytes)}, blobSizes)

	nodeID2 := types.RandomNodeID()
	ballot2 := types.NewExistingBallot(types.BallotID{2}, types.RandomEdSignature(), nodeID2, types.LayerID(0))
	require.NoError(t, ballots.Add(db, &ballot2))
	proposal2 := &types.Proposal{
		InnerProposal: types.InnerProposal{
			Ballot:   ballot2,
			TxIDs:    []types.TransactionID{{5, 6}, {7, 8}},
			MeshHash: types.RandomHash(),
		},
		Signature: types.RandomEdSignature(),
	}
	proposal2.SetID(types.ProposalID{9, 10})
	require.NoError(t, Add(db, proposal2))

	var blob2 sql.Blob
	require.NoError(t, LoadBlob(db, proposal2.ID().Bytes(), &blob2))
	encoded, err = codec.Encode(proposal2)
	require.NoError(t, err)
	require.Equal(t, encoded, blob2.Bytes)
	blobSizes, err = GetBlobSizes(db, [][]byte{proposal1.ID().Bytes(), proposal2.ID().Bytes()})
	require.NoError(t, err)
	require.Equal(t, []int{len(blob1.Bytes), len(blob2.Bytes)}, blobSizes)

	noSuchID := types.RandomProposalID()
	require.ErrorIs(t, LoadBlob(db, noSuchID[:], &sql.Blob{}), sql.ErrNotFound)

	blobSizes, err = GetBlobSizes(db, [][]byte{
		proposal1.ID().Bytes(),
		noSuchID.Bytes(),
		proposal2.ID().Bytes(),
	})
	require.NoError(t, err)
	require.Equal(t, []int{len(blob1.Bytes), -1, len(blob2.Bytes)}, blobSizes)
}
