package wallet

import (
	"bytes"
	"testing"

	"github.com/oasisprotocol/curve25519-voi/primitives/ed25519"
	"github.com/spacemeshos/go-scale"
	"github.com/stretchr/testify/require"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/common/util"
	"github.com/spacemeshos/go-spacemesh/genvm/core"
)

const (
	TestGenesisId = "1e459123cf26ee0de56d9e2ef6a30f8317c797e1"
)

func FuzzVerify(f *testing.F) {
	f.Fuzz(func(t *testing.T, data []byte) {
		wallet := Wallet{}
		dec := scale.NewDecoder(bytes.NewReader(data))
		wallet.Verify(&core.Context{}, data, dec)
	})
}

func TestMaxSpend(t *testing.T) {
	wallet := Wallet{}
	t.Run("Spawn", func(t *testing.T) {
		max, err := wallet.MaxSpend(0, &SpawnArguments{})
		require.NoError(t, err)
		require.EqualValues(t, 0, max)
	})
	t.Run("Spend", func(t *testing.T) {
		const amount = 100
		max, err := wallet.MaxSpend(1, &SpendArguments{Amount: amount})
		require.NoError(t, err)
		require.EqualValues(t, amount, max)
	})
}

func TestVerify(t *testing.T) {
	pub, pk, err := ed25519.GenerateKey(nil)
	require.NoError(t, err)
	spawn := &SpawnArguments{}
	copy(spawn.PublicKey[:], pub)
	wallet := New(spawn)

	ctx := core.Context{}
	var id [20]byte
	copy(id[:], util.Hex2Bytes(TestGenesisId))
	ctx.GenesisID = id

	t.Run("Invalid", func(t *testing.T) {
		buf64 := types.Bytes64{}
		require.False(t, wallet.Verify(&ctx, buf64[:], scale.NewDecoder(bytes.NewReader(buf64[:]))))
	})
	t.Run("Empty", func(t *testing.T) {
		require.False(t, wallet.Verify(&ctx, nil, scale.NewDecoder(bytes.NewBuffer(nil))))
	})
	t.Run("Valid", func(t *testing.T) {
		msg := []byte{1, 2, 3}
		hash := core.Hash(ctx.GenesisID[:], msg)
		sig := ed25519.Sign(pk, hash[:])
		require.True(t, wallet.Verify(&ctx, append(msg, sig...), scale.NewDecoder(bytes.NewReader(sig))))
	})
}
