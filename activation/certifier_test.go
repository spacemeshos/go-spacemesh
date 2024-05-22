package activation

import (
	"context"
	"net/url"
	"testing"

	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
	"go.uber.org/zap/zaptest"
	"golang.org/x/sync/errgroup"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/sql"
	"github.com/spacemeshos/go-spacemesh/sql/atxs"
	"github.com/spacemeshos/go-spacemesh/sql/localsql"
	certdb "github.com/spacemeshos/go-spacemesh/sql/localsql/certifier"
	"github.com/spacemeshos/go-spacemesh/sql/localsql/nipost"
)

func TestPersistsCerts(t *testing.T) {
	client := NewMockcertifierClient(gomock.NewController(t))
	id := types.RandomNodeID()
	db := localsql.InMemory()
	cert := &certdb.PoetCert{Data: []byte("cert"), Signature: []byte("sig")}
	certifierAddress := &url.URL{Scheme: "http", Host: "certifier.org"}
	pubkey := []byte("pubkey")
	{
		c := NewCertifier(db, zaptest.NewLogger(t), client)
		client.EXPECT().
			Certificate(gomock.Any(), id, certifierAddress, pubkey).
			Return(cert, nil)

		_, err := certdb.Certificate(db, id, pubkey)
		require.ErrorIs(t, err, sql.ErrNotFound)
		got, err := c.Certificate(context.Background(), id, certifierAddress, pubkey)
		require.NoError(t, err)
		require.Equal(t, cert, got)

		got, err = c.Certificate(context.Background(), id, certifierAddress, pubkey)
		require.NoError(t, err)
		require.Equal(t, cert, got)

		got, err = certdb.Certificate(db, id, pubkey)
		require.NoError(t, err)
		require.Equal(t, cert, got)
	}
	{
		// Create new certifier and check that it loads the certs back.
		c := NewCertifier(db, zaptest.NewLogger(t), client)
		got, err := c.Certificate(context.Background(), id, certifierAddress, pubkey)
		require.NoError(t, err)
		require.Equal(t, cert, got)
	}
}

func TestAvoidsRedundantQueries(t *testing.T) {
	client := NewMockcertifierClient(gomock.NewController(t))
	id := types.RandomNodeID()
	db := localsql.InMemory()
	cert := &certdb.PoetCert{Data: []byte("cert"), Signature: []byte("sig")}
	certifierAddress := &url.URL{Scheme: "http", Host: "certifier.org"}
	pubkey := []byte("pubkey")

	c := NewCertifier(db, zaptest.NewLogger(t), client)
	client.EXPECT().
		Certificate(gomock.Any(), id, certifierAddress, pubkey).
		Return(cert, nil)

	var eg errgroup.Group
	for i := 0; i < 100; i++ {
		eg.Go(func() error {
			got, err := c.Certificate(context.Background(), id, certifierAddress, pubkey)
			require.NoError(t, err)
			require.Equal(t, cert, got)
			return nil
		})
	}
	eg.Wait()

	got, err := certdb.Certificate(db, id, pubkey)
	require.NoError(t, err)
	require.Equal(t, cert, got)
}

func TestObtainingPost(t *testing.T) {
	id := types.RandomNodeID()

	t.Run("no POST or ATX", func(t *testing.T) {
		db := sql.InMemory()
		localDb := localsql.InMemory()

		certifier := NewCertifierClient(db, localDb, zaptest.NewLogger(t))
		_, err := certifier.obtainPost(context.Background(), id)
		require.ErrorContains(t, err, "PoST not found")
	})
	t.Run("initial POST available", func(t *testing.T) {
		db := sql.InMemory()
		localDb := localsql.InMemory()

		post := nipost.Post{
			Nonce:         30,
			Indices:       types.RandomBytes(20),
			Pow:           17,
			Challenge:     types.RandomBytes(32),
			NumUnits:      2,
			CommitmentATX: types.RandomATXID(),
			VRFNonce:      15,
		}
		err := nipost.AddPost(localDb, id, post)
		require.NoError(t, err)

		certifier := NewCertifierClient(db, localDb, zaptest.NewLogger(t))
		got, err := certifier.obtainPost(context.Background(), id)
		require.NoError(t, err)
		require.Equal(t, post, *got)
	})
	t.Run("initial POST unavailable but ATX exists", func(t *testing.T) {
		db := sql.InMemory()
		localDb := localsql.InMemory()

		atx := newInitialATXv1(t, types.RandomATXID())
		atx.SmesherID = id
		require.NoError(t, atxs.Add(db, toAtx(t, atx)))

		certifier := NewCertifierClient(db, localDb, zaptest.NewLogger(t))
		got, err := certifier.obtainPost(context.Background(), id)
		require.NoError(t, err)
		require.Equal(t, atx.NIPost.Post.Indices, got.Indices)
		require.Equal(t, atx.NIPost.Post.Nonce, got.Nonce)
		require.Equal(t, atx.NIPost.Post.Pow, got.Pow)
		require.Equal(t, atx.NIPost.PostMetadata.Challenge, got.Challenge)
		require.Equal(t, atx.NumUnits, got.NumUnits)
		require.Equal(t, atx.CommitmentATXID, &got.CommitmentATX)
	})
}
