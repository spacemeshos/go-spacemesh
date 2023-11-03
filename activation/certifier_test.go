package activation_test

import (
	"context"
	"net/url"
	"testing"

	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
	"go.uber.org/zap/zaptest"

	"github.com/spacemeshos/go-spacemesh/activation"
)

func TestPersistsCerts(t *testing.T) {
	client := activation.NewMockcertifierClient(gomock.NewController(t))
	datadir := t.TempDir()

	{
		certifier := activation.NewCertifier(datadir, zaptest.NewLogger(t), client)

		poetMock := activation.NewMockPoetClient(gomock.NewController(t))
		poetMock.EXPECT().Address().Return("http://poet")
		poetMock.EXPECT().CertifierInfo(gomock.Any()).Return(&activation.CertifierInfo{
			URL:    &url.URL{Scheme: "http", Host: "certifier.org"},
			PubKey: []byte("pubkey"),
		}, nil)

		client.EXPECT().
			Certify(gomock.Any(), &url.URL{Scheme: "http", Host: "certifier.org"}, []byte("pubkey")).
			Return(&activation.PoetCert{Signature: []byte("cert")}, nil)

		require.Nil(t, certifier.GetCertificate("http://poet"))
		certs, err := certifier.Recertify(context.Background(), poetMock)
		require.NoError(t, err)
		require.Equal(t, []byte("cert"), certs.Signature)

		cert := certifier.GetCertificate("http://poet")
		require.Equal(t, []byte("cert"), cert.Signature)
		require.Nil(t, certifier.GetCertificate("http://other-poet"))
	}
	{
		// Create new certifier and check that it loads the certs back.
		certifier := activation.NewCertifier(datadir, zaptest.NewLogger(t), client)
		cert := certifier.GetCertificate("http://poet")
		require.Equal(t, []byte("cert"), cert.Signature)
	}
}
