package activation_test

import (
	"bytes"
	"context"
	"testing"

	"github.com/stretchr/testify/require"
	"golang.org/x/sync/errgroup"

	"github.com/spacemeshos/go-spacemesh/activation"
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/signing"
)

func TestHTTPPoet(t *testing.T) {
	if testing.Short() {
		t.Skip()
	}
	t.Parallel()
	r := require.New(t)

	var eg errgroup.Group

	poetDir := t.TempDir()
	t.Cleanup(func() { r.NoError(eg.Wait()) })

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	c, err := activation.NewHTTPPoetTestHarness(ctx, poetDir)
	r.NoError(err)
	r.NotNil(c)

	eg.Go(func() error {
		return c.Service.Start(ctx)
	})

	signer, err := signing.NewEdSigner(signing.WithPrefix([]byte("prefix")))
	require.NoError(t, err)
	ch := types.RandomHash()

	signature := signer.Sign(signing.POET, ch.Bytes())
	prefix := bytes.Join([][]byte{signer.Prefix(), {byte(signing.POET)}}, nil)

	poetRound, err := c.Submit(context.Background(), prefix, ch.Bytes(), signature, signer.NodeID(), activation.PoetPoW{})
	r.NoError(err)
	r.NotNil(poetRound)
}
