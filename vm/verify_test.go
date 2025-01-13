package vm

import (
	"testing"

	"github.com/stretchr/testify/require"
	"go.uber.org/zap/zaptest"

	"github.com/spacemeshos/go-spacemesh/codec"
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/signing"
	"github.com/spacemeshos/go-spacemesh/sql/statesql"
	"github.com/spacemeshos/go-spacemesh/vm/core"
	sdkwallet "github.com/spacemeshos/go-spacemesh/vm/sdk/wallet"
	"github.com/spacemeshos/go-spacemesh/vm/templates/wallet"
)

func TestVerify(t *testing.T) {
	genesisID := types.RandomHash().ToHash20()
	signer, err := signing.NewEdSigner()
	require.NoError(t, err)
	pubkey := signer.PublicKey()
	principal := sdkwallet.Address(pubkey.Bytes())

	t.Run("Invalid", func(t *testing.T) {
		logger := zaptest.NewLogger(t)
		loader := core.DBLoader{Executor: statesql.InMemoryTest(t)}
		ctx, err := core.New(genesisID, 0, principal, loader, logger)
		require.NoError(t, err)
		ctx.Header.MaxGas = 100_000
		ctx.TxData = []byte("some data")
		ctx.TemplateCode = wallet.PROGRAM
		require.Error(t, verify(ctx, logger))
	})
	t.Run("Empty", func(t *testing.T) {
		logger := zaptest.NewLogger(t)
		loader := core.DBLoader{Executor: statesql.InMemoryTest(t)}
		ctx, err := core.New(genesisID, 0, principal, loader, logger)
		require.NoError(t, err)
		ctx.Header.MaxGas = 100_000
		ctx.TxData = []byte("some data")
		ctx.TemplateCode = wallet.PROGRAM
		ctx.TxData = nil
		require.Error(t, verify(ctx, logger))
	})
	t.Run("Valid", func(t *testing.T) {
		logger := zaptest.NewLogger(t)
		loader := core.DBLoader{Executor: statesql.InMemoryTest(t)}
		ctx, err := core.New(genesisID, 0, principal, loader, logger)
		require.NoError(t, err)
		ctx.PrincipalAccount.State = pubkey.Bytes()
		ctx.Header.MaxGas = 100_000
		ctx.TxData = []byte("some data")
		ctx.TemplateCode = wallet.PROGRAM
		ctx.WitnessData = core.SignRawTx(ctx.TxData, genesisID, signer.PrivateKey())
		require.NoError(t, verify(ctx, logger))
	})
	t.Run("spawn TX", func(t *testing.T) {
		logger := zaptest.NewLogger(t)
		loader := core.DBLoader{Executor: statesql.InMemoryTest(t)}
		tx, err := sdkwallet.SpawnTx(signing.Public(signer.PrivateKey()), 6)
		require.NoError(t, err)
		ctx, err := core.New(genesisID, 0, principal, loader, logger)
		require.NoError(t, err)
		ctx.TxData = codec.MustEncode(tx)
		ctx.TxPayload = tx.Payload
		ctx.WitnessData = core.SignRawTx(ctx.TxData, genesisID, signer.PrivateKey())
		ctx.TemplateCode = wallet.PROGRAM
		ctx.SpawnTx = true
		ctx.Header = core.Header{
			TemplateAddress: wallet.TemplateAddress,
			MaxGas:          100000000,
		}
		require.NoError(t, verify(ctx, logger))
	})
}
