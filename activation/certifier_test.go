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
	"github.com/spacemeshos/go-spacemesh/sql/localsql/certifier"
)

func TestPersistsCerts(t *testing.T) {
	client := activation.NewMockcertifierClient(gomock.NewController(t))
	client.EXPECT().Id().AnyTimes().Return(types.RandomNodeID())
	db := localsql.InMemory()
	cert := &certifier.PoetCert{Data: []byte("cert"), Signature: []byte("sig")}
	{
		c := activation.NewCertifier(db, zaptest.NewLogger(t), client)

		poetMock := activation.NewMockPoetClient(gomock.NewController(t))
		poetMock.EXPECT().Address().Return("http://poet")
		poetMock.EXPECT().
			CertifierInfo(gomock.Any()).
			Return(&url.URL{Scheme: "http", Host: "certifier.org"}, []byte("pubkey"), nil)

		client.EXPECT().
			Certify(gomock.Any(), &url.URL{Scheme: "http", Host: "certifier.org"}, []byte("pubkey")).
			Return(cert, nil)

		require.Nil(t, c.Certificate("http://poet"))
		got, err := c.Recertify(context.Background(), poetMock)
		require.NoError(t, err)
		require.Equal(t, cert, got)

		got = c.Certificate("http://poet")
		require.Equal(t, cert, got)
		require.Nil(t, c.Certificate("http://other-poet"))
	}
	{
		// Create new certifier and check that it loads the certs back.
		c := activation.NewCertifier(db, zaptest.NewLogger(t), client)
		got := c.Certificate("http://poet")
		require.Equal(t, cert, got)
	}
}
