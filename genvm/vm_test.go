package vm

import (
	"bytes"
	"testing"

	"github.com/oasisprotocol/curve25519-voi/primitives/ed25519"
	"github.com/spacemeshos/go-scale"
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/genvm/core"
	"github.com/spacemeshos/go-spacemesh/genvm/templates/wallet"
	"github.com/spacemeshos/go-spacemesh/sql"
	"github.com/spacemeshos/go-spacemesh/sql/accounts"
	"github.com/stretchr/testify/require"
)

func encodeWalletTx(tb testing.TB, pk ed25519.PrivateKey, fields ...scale.Encodable) []byte {
	tb.Helper()

	buf := bytes.NewBuffer(nil)
	encoder := scale.NewEncoder(buf)
	for _, field := range fields {
		_, err := field.EncodeScale(encoder)
		require.NoError(tb, err)
	}
	hash := core.Hash(buf.Bytes())

	sig := ed25519.Sign(pk, hash[:])
	var sigfield scale.Signature
	copy(sigfield[:], sig)
	_, err := sigfield.EncodeScale(encoder)
	require.NoError(tb, err)
	return buf.Bytes()
}

func TestWallet(t *testing.T) {
	db := sql.InMemory()

	pks := make([]ed25519.PrivateKey, 10)
	pubs := make([]ed25519.PublicKey, len(pks))
	addresses := make([]core.Address, len(pks))

	for i := range pks {
		pub, pk, err := ed25519.GenerateKey(nil)
		require.NoError(t, err)
		pks[i] = pk
		pubs[i] = pub
		args := wallet.SpawnArguments{}
		copy(args.PublicKey[:], pub)
		addresses[i] = ComputePrincipal(wallet.TemplateAddress, core.Nonce{}, &args)

		require.NoError(t, accounts.Update(db, &types.Account{
			Address: types.Address(addresses[i]),
			Balance: 100000000000000,
		}))
	}

	vm := New(db)

	typ := scale.U8(0)
	method := scale.U8(0)
	payload := wallet.SpawnPayload{GasPrice: 1}
	copy(payload.Arguments.PublicKey[:], pubs[0])

	spawn := encodeWalletTx(t, pks[0],
		&typ,
		&addresses[0], &method, &wallet.TemplateAddress,
		&payload,
	)
	skipped, err := vm.Apply(types.NewLayerID(1), [][]byte{spawn})
	require.NoError(t, err)
	require.Empty(t, skipped)
}
