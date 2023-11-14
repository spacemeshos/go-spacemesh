package certifier_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/sql/localsql"
	"github.com/spacemeshos/go-spacemesh/sql/localsql/certifier"
)

func TestAddingCertificates(t *testing.T) {
	db := localsql.InMemory()
	nodeId := types.RandomNodeID()

	require.NoError(t, certifier.AddCertificate(db, nodeId, []byte("cert"), "poet-0"))
	cert, err := certifier.Certificate(db, nodeId, "poet-0")
	require.NoError(t, err)
	require.Equal(t, []byte("cert"), cert)

	require.NoError(t, certifier.AddCertificate(db, nodeId, []byte("cert2"), "poet-1"))

	cert, err = certifier.Certificate(db, nodeId, "poet-1")
	require.NoError(t, err)
	require.Equal(t, []byte("cert2"), cert)
	cert, err = certifier.Certificate(db, nodeId, "poet-0")
	require.NoError(t, err)
	require.Equal(t, []byte("cert"), cert)
}

func TestOverwritingCertificates(t *testing.T) {
	db := localsql.InMemory()
	nodeId := types.RandomNodeID()

	require.NoError(t, certifier.AddCertificate(db, nodeId, []byte("cert"), "poet-0"))
	cert, err := certifier.Certificate(db, nodeId, "poet-0")
	require.NoError(t, err)
	require.Equal(t, []byte("cert"), cert)

	require.NoError(t, certifier.AddCertificate(db, nodeId, []byte("cert2"), "poet-0"))
	cert, err = certifier.Certificate(db, nodeId, "poet-0")
	require.NoError(t, err)
	require.Equal(t, []byte("cert2"), cert)
}
