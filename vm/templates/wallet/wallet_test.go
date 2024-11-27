package wallet

import (
	"bytes"
	"encoding/hex"
	"testing"

	athcon "github.com/athenavm/athena/ffi/athcon/bindings/go"
	"github.com/oasisprotocol/curve25519-voi/primitives/ed25519"
	"github.com/spacemeshos/go-scale"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/vm/core"
	"github.com/spacemeshos/go-spacemesh/vm/core/mocks"
	"github.com/spacemeshos/go-spacemesh/vm/host"
)

const (
	PUBKEY  = "ba216991978cab901254e8eaa062830bbe42c6fc7f56032cbed0e8926ad43e97"
	PRIVKEY = "2375b169ab93821366eb5e6898145ec12b6419536b8ee0615cae783b4bc015e7" +
		"ba216991978cab901254e8eaa062830bbe42c6fc7f56032cbed0e8926ad43e97"
	PRINCIPAL    = "00000000DF39133A6A5B6DDBFEBC865F05640671F00A3930"
	WALLET_STATE = "ba216991978cab901254e8eaa062830bbe42c6fc7f56032cbed0e8926ad43e97"
)

func FuzzVerify(f *testing.F) {
	f.Fuzz(func(t *testing.T, data []byte) {
		wallet := Wallet{}
		dec := scale.NewDecoder(bytes.NewReader(data))
		wallet.Verify(data, dec)
	})
}

func TestMaxSpend(t *testing.T) {
	const amount = 100

	ctrl := gomock.NewController(t)
	testWallet := Wallet{}
	mockHost := mocks.NewMockHost(ctrl)
	testWallet.host = mockHost
	testWallet.templateCode = PROGRAM
	walletState, err := hex.DecodeString(WALLET_STATE)
	require.NoError(t, err)
	testWallet.walletState = walletState

	libPath, err := host.AthenaLibPath()
	require.NoError(t, err)
	vmlib, err := athcon.LoadLibrary(libPath)
	require.NoError(t, err)

	// construct spawn and spend payloads
	spawnPayload := vmlib.EncodeTxSpawn(athcon.Bytes32{})
	spendPayload := vmlib.EncodeTxSpend(athcon.Address{}, amount)

	mockHost.EXPECT().Principal().Return(types.Address{}).Times(5)
	mockHost.EXPECT().MaxGas().Return(100000).Times(2)
	mockHost.EXPECT().Clone().Return(mockHost)
	mockHost.EXPECT().Nonce()
	mockHost.EXPECT().TemplateAddress().Return(types.Address{})
	mockHost.EXPECT().Layer().Return(core.LayerID(1))
	t.Run("Spawn", func(t *testing.T) {
		max, err := testWallet.MaxSpend(spawnPayload)
		require.NoError(t, err)
		require.EqualValues(t, 0, max)
	})
	t.Run("Spend", func(t *testing.T) {
		max, err := testWallet.MaxSpend(spendPayload)
		require.NoError(t, err)
		require.EqualValues(t, amount, max)
	})
}

func TestSpawn(t *testing.T) {
	ctrl := gomock.NewController(t)
	mockHost := mocks.NewMockHost(ctrl)

	principalAddress := types.Address{1}
	templateAddress := types.Address{2}
	principalBytes, err := hex.DecodeString(PRINCIPAL)
	require.NoError(t, err)
	expectedPrincipalAddress := types.Address(principalBytes)
	pubkeyBytes, err := hex.DecodeString(PUBKEY)
	require.NoError(t, err)
	pubkey := athcon.Bytes32(pubkeyBytes)

	mockTemplate := types.Account{
		State: PROGRAM,
	}

	const maxGas = 100_000
	var spendGas uint64
	mockHost.EXPECT().Layer().Return(core.LayerID(1))
	mockHost.EXPECT().Principal().Return(principalAddress).Times(6)
	mockHost.EXPECT().MaxGas().Return(maxGas)
	mockHost.EXPECT().SpendGas(gomock.Any()).Do(func(g uint64) { spendGas = g })
	mockHost.EXPECT().TemplateAddress().Return(templateAddress).Times(2)
	mockHost.EXPECT().Nonce()
	mockHost.EXPECT().IsSpawn().Return(true)
	mockHost.EXPECT().GasSpent()
	mockHost.EXPECT().Get(templateAddress).Return(&mockTemplate, nil)
	mockHost.EXPECT().Get(principalAddress).Return(&types.Account{}, nil)
	mockHost.EXPECT().Spawn(gomock.Any(), gomock.Any()).Return(expectedPrincipalAddress, nil)

	// point to the library path
	libPath, err := host.AthenaLibPath()
	require.NoError(t, err)
	vmLib, err := athcon.LoadLibrary(libPath)
	require.NoError(t, err)

	athenaPayload := vmLib.EncodeTxSpawn(athcon.Bytes32(pubkey))

	// Execute the spawn and catch the result
	output, gasLeft, err := (&handler{}).Exec(mockHost, athenaPayload)
	require.Equal(t, int64(maxGas-spendGas), gasLeft)
	require.Len(t, output, 24)
	require.Equal(t, expectedPrincipalAddress, types.Address(output))
	require.NoError(t, err)
}

func TestVerify(t *testing.T) {
	walletState, err := hex.DecodeString(WALLET_STATE)
	require.NoError(t, err)
	privkeyBytes, err := hex.DecodeString(PRIVKEY)
	require.NoError(t, err)
	pubkeyBytes, err := hex.DecodeString(PUBKEY)
	require.NoError(t, err)

	require.Equal(t, ed25519.PrivateKey(privkeyBytes).Public().(ed25519.PublicKey), ed25519.PublicKey(pubkeyBytes))

	ctrl := gomock.NewController(t)
	mockHost := mocks.NewMockHost(ctrl)

	mockTemplate := types.Account{
		State: PROGRAM,
	}
	mockWallet := types.Account{
		State: walletState,
	}

	// Times counts the total number of times these methods are called.
	// Note that wallet.Verify() short-circuits when called on empty input, so it only actually
	// runs twice.
	mockHost.EXPECT().Layer().Return(core.LayerID(1)).Times(2)
	mockHost.EXPECT().Principal().Return(types.Address{2}).Times(11)
	mockHost.EXPECT().MaxGas().Return(100000000).Times(2)
	mockHost.EXPECT().TemplateAddress().Return(types.Address{1}).Times(3)
	mockHost.EXPECT().Get(types.Address{1}).Return(&mockTemplate, nil)
	mockHost.EXPECT().Get(types.Address{2}).Return(&mockWallet, nil)
	mockHost.EXPECT().IsSpawn().Return(false).Times(3)
	mockHost.EXPECT().Clone().Return(mockHost).Times(2)
	mockHost.EXPECT().Nonce().Times(2)
	mockHost.EXPECT().SpendGas(gomock.Any())
	mockHost.EXPECT().SpendGas(gomock.Any())

	// for now, don't include GenesisID
	// empty := types.Hash20{}
	// mockHost.EXPECT().GetGenesisID().Return(empty).Times(3)

	wallet, err := New(mockHost)
	require.NoError(t, err)

	t.Run("Invalid", func(t *testing.T) {
		buf64 := types.EdSignature{}
		require.False(t, wallet.Verify(buf64[:], scale.NewDecoder(bytes.NewReader(buf64[:]))))
	})
	t.Run("Empty", func(t *testing.T) {
		require.False(t, wallet.Verify(nil, scale.NewDecoder(bytes.NewBuffer(nil))))
	})
	t.Run("Valid", func(t *testing.T) {
		msg := []byte{1, 2, 3}
		sig := ed25519.Sign(privkeyBytes, msg)
		require.True(
			t,
			wallet.Verify(append(msg, sig...), scale.NewDecoder(bytes.NewReader(sig))),
		)
	})
}
