package activation_test

import (
	"context"
	"net/url"
	"testing"

	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
	"go.uber.org/zap/zaptest"

	"github.com/spacemeshos/go-spacemesh/activation"
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/sql/localsql"
)

func TestPersistsCerts(t *testing.T) {
	client := activation.NewMockcertifierClient(gomock.NewController(t))
	client.EXPECT().Id().AnyTimes().Return(types.RandomNodeID())
	db := localsql.InMemory()
	{
		certifier := activation.NewCertifier(db, zaptest.NewLogger(t), client)

		poetMock := activation.NewMockPoetClient(gomock.NewController(t))
		poetMock.EXPECT().Address().Return("http://poet")
		poetMock.EXPECT().
			CertifierInfo(gomock.Any()).
			Return(&url.URL{Scheme: "http", Host: "certifier.org"}, []byte("pubkey"), nil)

		client.EXPECT().
			Certify(gomock.Any(), &url.URL{Scheme: "http", Host: "certifier.org"}, []byte("pubkey")).
			Return(activation.PoetCert("cert"), nil)

		require.Nil(t, certifier.GetCertificate("http://poet"))
		certs, err := certifier.Recertify(context.Background(), poetMock)
		require.NoError(t, err)
		require.Equal(t, activation.PoetCert("cert"), certs)

		cert := certifier.GetCertificate("http://poet")
		require.Equal(t, activation.PoetCert("cert"), cert)
		require.Nil(t, certifier.GetCertificate("http://other-poet"))
	}
	{
		// Create new certifier and check that it loads the certs back.
		certifier := activation.NewCertifier(db, zaptest.NewLogger(t), client)
		cert := certifier.GetCertificate("http://poet")
		require.Equal(t, activation.PoetCert("cert"), cert)
	}
}

func TestSeedWithCerts(t *testing.T) {
	client := activation.NewMockcertifierClient(gomock.NewController(t))
	client.EXPECT().Id().AnyTimes().Return(types.RandomNodeID())

	certs := []activation.Certificate{
		{
			Poet:        "poet1",
			Certificate: activation.Base64Enc{Inner: []byte("cert1")},
		},
		{
			Poet:        "poet2",
			Certificate: activation.Base64Enc{Inner: []byte("cert2")},
		},
	}
	c := activation.NewCertifier(localsql.InMemory(), zaptest.NewLogger(t), client, activation.WithCertificates(certs))
	require.Equal(t, activation.PoetCert("cert1"), c.GetCertificate("poet1"))
	require.Equal(t, activation.PoetCert("cert2"), c.GetCertificate("poet2"))
}
