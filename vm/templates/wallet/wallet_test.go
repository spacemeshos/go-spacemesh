package wallet

import (
	"bytes"
	"encoding/binary"
	"testing"

	// "github.com/oasisprotocol/curve25519-voi/primitives/ed25519"
	"github.com/spacemeshos/go-scale"
	"github.com/stretchr/testify/require"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/vm/core"
	"github.com/spacemeshos/go-spacemesh/vm/core/mocks"

	"go.uber.org/mock/gomock"

	athcon "github.com/athenavm/athena/ffi/athcon/bindings/go"
)

func FuzzVerify(f *testing.F) {
	f.Fuzz(func(t *testing.T, data []byte) {
		wallet := Wallet{}
		dec := scale.NewDecoder(bytes.NewReader(data))
		wallet.Verify(&core.Context{}, data, dec)
	})
}

type testWallet struct {
	*Wallet
	mockHost   *mocks.MockHost
	mockVMHost *mocks.MockVMHost
}

func TestMaxSpend(t *testing.T) {
	ctrl := gomock.NewController(t)
	wallet := Wallet{}
	testWallet := testWallet{}
	testWallet.Wallet = &wallet
	mockHost := mocks.NewMockHost(ctrl)
	mockVMHost := mocks.NewMockVMHost(ctrl)
	testWallet.host = mockHost
	testWallet.mockHost = mockHost
	testWallet.vmhost = mockVMHost
	testWallet.mockVMHost = mockVMHost

	// construct spawn and spend payloads
	// nothing in the payload after the selector matters
	spawnPayload, _ := athcon.FromString("athexp_spawn")
	spendPayload, _ := athcon.FromString("athexp_spend")

	output := make([]byte, 8)
	const amount = 100
	binary.LittleEndian.PutUint64(output, amount)
	testWallet.mockHost.EXPECT().Layer().Return(core.LayerID(1)).Times(1)
	testWallet.mockHost.EXPECT().Principal().Return(types.Address{}).Times(2)
	testWallet.mockHost.EXPECT().MaxGas().Return(1000).Times(2)
	testWallet.mockVMHost.EXPECT().Execute(
		gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(),
	).Return(output, 0, nil).Times(1)
	t.Run("Spawn", func(t *testing.T) {
		max, err := testWallet.MaxSpend(spawnPayload[:])
		require.NoError(t, err)
		require.EqualValues(t, 0, max)
	})
	t.Run("Spend", func(t *testing.T) {
		max, err := testWallet.MaxSpend(spendPayload[:])
		require.NoError(t, err)
		require.EqualValues(t, amount, max)
	})
}

// func TestVerify(t *testing.T) {
// 	pub, pk, err := ed25519.GenerateKey(nil)
// 	require.NoError(t, err)
// 	spawn := &SpawnArguments{}
// 	copy(spawn.PublicKey[:], pub)
// 	wallet := New(spawn)

// 	t.Run("Invalid", func(t *testing.T) {
// 		buf64 := types.EdSignature{}
// 		require.False(t, wallet.Verify(&core.Context{}, buf64[:], scale.NewDecoder(bytes.NewReader(buf64[:]))))
// 	})
// 	t.Run("Empty", func(t *testing.T) {
// 		require.False(t, wallet.Verify(&core.Context{}, nil, scale.NewDecoder(bytes.NewBuffer(nil))))
// 	})
// 	t.Run("Valid", func(t *testing.T) {
// 		msg := []byte{1, 2, 3}
// 		empty := types.Hash20{}
// 		body := core.SigningBody(empty[:], msg)
// 		sig := ed25519.Sign(pk, body[:])
// 		require.True(
// 			t,
// 			wallet.Verify(&core.Context{GenesisID: empty}, append(msg, sig...), scale.NewDecoder(bytes.NewReader(sig))),
// 		)
// 	})
// }
