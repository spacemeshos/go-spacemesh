package certifiedblocks_test

import (
    certtypes "github.com/spacemeshos/go-spacemesh/blockcerts/types"
    "github.com/spacemeshos/go-spacemesh/common/types"
    "github.com/spacemeshos/go-spacemesh/sql"
    "github.com/spacemeshos/go-spacemesh/sql/certifiedblocks"
    "github.com/stretchr/testify/require"
    "testing"
)

func TestAddGet(t *testing.T) {
    db := sql.InMemory()
    cert := &certtypes.BlockCertificate{
        BlockID: types.RandomBlockID(),
        LayerID: types.LayerID{Value: 42},
        TerminationSignatures: []certtypes.BlockSignature{
            {
                SignerNodeID:         types.NodeID{},
                SignerRoleProof:      []byte{1},
                SignerCommitteeSeats: 8,
            },
        },
    }

    require.NoError(t, certifiedblocks.Add(db, cert))
    got, err := certifiedblocks.Get(db, cert.BlockID)
    require.NoError(t, err)
    require.Equal(t, *cert, *got)
}

func TestHas(t *testing.T) {
    db := sql.InMemory()
    cert := &certtypes.BlockCertificate{
        BlockID: types.RandomBlockID(),
        LayerID: types.LayerID{Value: 42},
        TerminationSignatures: []certtypes.BlockSignature{
            {
                SignerNodeID:         types.NodeID{},
                SignerRoleProof:      []byte{1},
                SignerCommitteeSeats: 8,
            },
        },
    }

    exists, err := certifiedblocks.Has(db, cert.BlockID)
    require.NoError(t, err)
    require.False(t, exists)

    require.NoError(t, certifiedblocks.Add(db, cert))
    exists, err = certifiedblocks.Has(db, cert.BlockID)
    require.NoError(t, err)
    require.True(t, exists)
}
