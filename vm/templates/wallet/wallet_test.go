package wallet

import (
	"encoding/hex"
	"testing"

	athcon "github.com/athenavm/athena/ffi/athcon/bindings/go"
	"github.com/oasisprotocol/curve25519-voi/primitives/ed25519"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
	"go.uber.org/zap/zaptest"

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
	f.Fuzz(func(t *testing.T, data, witnessData []byte) {
		wallet := Wallet{}
		wallet.Verify(data, witnessData)
	})
}

func TestMaxSpend(t *testing.T) {
	const amount = 100
	principalAddress := types.Address{1, 2, 3, 4}
	walletState, err := hex.DecodeString(WALLET_STATE)
	require.NoError(t, err)

	ctrl := gomock.NewController(t)
	mockHost := mocks.NewMockHost(ctrl)
	mockHost.EXPECT().IsSpawn().Return(false)
	mockHost.EXPECT().Principal().Return(principalAddress).AnyTimes()
	mockHost.EXPECT().TemplateAddress().Return(TemplateAddress).AnyTimes()
	mockHost.EXPECT().Get(TemplateAddress).Return(&types.Account{Address: TemplateAddress, State: PROGRAM}, nil)
	mockHost.EXPECT().Get(principalAddress).Return(&types.Account{Address: principalAddress, State: walletState}, nil)

	testWallet, err := New(mockHost, zaptest.NewLogger(t))
	require.NoError(t, err)

	libPath, err := host.AthenaLibPath()
	require.NoError(t, err)
	vmlib, err := athcon.LoadLibrary(libPath)
	require.NoError(t, err)
	defer vmlib.Close()

	// construct spawn and spend payloads
	spawnPayload := vmlib.EncodeTxSpawn(athcon.Bytes32{})
	spendPayload := vmlib.EncodeTxSpend(athcon.Address{}, amount)

	mockHost.EXPECT().MaxGas().Return(100000).Times(2)
	mockHost.EXPECT().Clone().Return(mockHost)
	mockHost.EXPECT().Layer().Return(core.LayerID(1)).AnyTimes()
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

	pubkey, _, err := ed25519.GenerateKey(nil)
	require.NoError(t, err)
	principalAddress := core.ComputePrincipalFromBlob(TemplateAddress, pubkey)

	const maxGas = 100_000
	mockHost.EXPECT().Principal().Return(principalAddress).Times(3)
	mockHost.EXPECT().TemplateAddress().Return(TemplateAddress)
	mockHost.EXPECT().Spawn(gomock.Any(), gomock.Any()).Return(principalAddress, nil)

	libPath, err := host.AthenaLibPath()
	require.NoError(t, err)
	vmLib, err := athcon.LoadLibrary(libPath)
	require.NoError(t, err)
	athenaPayload := vmLib.EncodeTxSpawn(athcon.Bytes32(pubkey))
	vmLib.Close()

	executionPayload := athcon.EncodedExecutionPayload(nil, athenaPayload)
	vmhost, err := host.NewHost(mockHost, zaptest.NewLogger(t))
	require.NoError(t, err)
	defer vmhost.Destroy()

	output, gasLeft, err := vmhost.Execute(0, maxGas, types.Address{}, types.Address{}, executionPayload, PROGRAM)
	require.Less(t, gasLeft, int64(maxGas))
	require.Len(t, output, 24)
	require.Equal(t, principalAddress, types.Address(output))
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

	mockHost.EXPECT().Get(types.Address{1}).Return(&mockTemplate, nil)
	mockHost.EXPECT().Get(types.Address{2}).Return(&mockWallet, nil)

	mockHost.EXPECT().Layer().Return(core.LayerID(1)).AnyTimes()
	mockHost.EXPECT().Principal().Return(types.Address{2}).AnyTimes()
	mockHost.EXPECT().MaxGas().Return(100000000).AnyTimes()
	mockHost.EXPECT().TemplateAddress().Return(types.Address{1}).AnyTimes()
	mockHost.EXPECT().IsSpawn().Return(false).AnyTimes()
	mockHost.EXPECT().Clone().Return(mockHost).AnyTimes()

	// for now, don't include GenesisID
	// empty := types.Hash20{}
	// mockHost.EXPECT().GetGenesisID().Return(empty).Times(3)

	wallet, err := New(mockHost, zaptest.NewLogger(t))
	require.NoError(t, err)

	t.Run("Invalid", func(t *testing.T) {
		buf64 := types.EdSignature{}
		mockHost.EXPECT().SpendGas(gomock.Any())
		require.Error(t, wallet.Verify(buf64[:], buf64[:]))
	})
	t.Run("Empty", func(t *testing.T) {
		mockHost.EXPECT().SpendGas(gomock.Any())
		require.Error(t, wallet.Verify(nil, nil))
	})
	t.Run("Valid", func(t *testing.T) {
		msg := []byte{1, 2, 3}
		sig := core.SignRawTx(msg, types.RandomHash().ToHash20(), privkeyBytes)

		mockHost.EXPECT().SpendGas(gomock.Any())
		require.NoError(t, wallet.Verify(msg, sig))
	})
}
