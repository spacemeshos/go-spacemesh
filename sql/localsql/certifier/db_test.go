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

	expCert := certifier.PoetCert{Data: []byte("data"), Signature: []byte("sig")}

	require.NoError(t, certifier.AddCertificate(db, nodeId, expCert, "poet-0"))
	cert, err := certifier.Certificate(db, nodeId, "poet-0")
	require.NoError(t, err)
	require.Equal(t, &expCert, cert)

	expCert2 := certifier.PoetCert{Data: []byte("data2"), Signature: []byte("sig2")}
	require.NoError(t, certifier.AddCertificate(db, nodeId, expCert2, "poet-1"))

	cert, err = certifier.Certificate(db, nodeId, "poet-1")
	require.NoError(t, err)
	require.Equal(t, &expCert2, cert)
	cert, err = certifier.Certificate(db, nodeId, "poet-0")
	require.NoError(t, err)
	require.Equal(t, &expCert, cert)
}

func TestOverwritingCertificates(t *testing.T) {
	db := localsql.InMemory()
	nodeId := types.RandomNodeID()

	expCert := certifier.PoetCert{Data: []byte("data"), Signature: []byte("sig")}
	require.NoError(t, certifier.AddCertificate(db, nodeId, expCert, "poet-0"))
	cert, err := certifier.Certificate(db, nodeId, "poet-0")
	require.NoError(t, err)
	require.Equal(t, &expCert, cert)

	expCert2 := certifier.PoetCert{Data: []byte("data2"), Signature: []byte("sig2")}
	require.NoError(t, certifier.AddCertificate(db, nodeId, expCert2, "poet-0"))
	cert, err = certifier.Certificate(db, nodeId, "poet-0")
	require.NoError(t, err)
	require.Equal(t, &expCert2, cert)
}
