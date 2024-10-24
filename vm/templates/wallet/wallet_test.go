package wallet

import (
	"bytes"
	"encoding/binary"
	"encoding/hex"
	"os"
	"testing"

	"github.com/oasisprotocol/curve25519-voi/primitives/ed25519"
	"github.com/spacemeshos/go-scale"
	"github.com/stretchr/testify/require"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/vm/core"
	"github.com/spacemeshos/go-spacemesh/vm/core/mocks"
	"github.com/spacemeshos/go-spacemesh/vm/host"
	walletTemplate "github.com/spacemeshos/go-spacemesh/vm/programs/wallet"

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
	mockHost.EXPECT().Layer().Return(core.LayerID(1)).Times(1)
	mockHost.EXPECT().Principal().Return(types.Address{}).Times(2)
	mockHost.EXPECT().MaxGas().Return(1000).Times(2)
	mockVMHost.EXPECT().Execute(
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

func TestSpawn(t *testing.T) {
	const PUBKEY = "ba216991978cab901254e8eaa062830bbe42c6fc7f56032cbed0e8926ad43e97"
	const PRINCIPAL = "00000000DF39133A6A5B6DDBFEBC865F05640671F00A3930"
	const WALLET_STATE = "00000000000000000000000000000000BA216991978CAB901254E8EAA062830BBE42C6FC7F56032CBED0E8926AD43E97"

	ctrl := gomock.NewController(t)
	mockHost := mocks.NewMockHost(ctrl)
	mockLoader := mocks.NewMockAccountLoader(ctrl)
	mockUpdater := mocks.NewMockAccountUpdater(ctrl)

	principalAddress := types.Address{1}
	templateAddress := types.Address{2}
	principalBytes, err := hex.DecodeString(PRINCIPAL)
	require.NoError(t, err)
	expectedPrincipalAddress := types.Address(principalBytes)
	pubkeyBytes, err := hex.DecodeString(PUBKEY)
	require.NoError(t, err)
	pubkey := athcon.Bytes32(pubkeyBytes)
	expectedWalletState, err := hex.DecodeString(WALLET_STATE)
	require.NoError(t, err)

	mockHost.EXPECT().Layer().Return(core.LayerID(1)).Times(1)
	mockHost.EXPECT().Principal().Return(principalAddress).Times(5)
	mockHost.EXPECT().MaxGas().Return(10000).Times(1)
	mockHost.EXPECT().TemplateAddress().Return(templateAddress).Times(2)
	mockHost.EXPECT().Nonce().Return(uint64(0)).Times(1)

	mockTemplate := types.Account{
		State: walletTemplate.PROGRAM,
	}
	mockLoader.EXPECT().Get(templateAddress).Return(mockTemplate, nil).Times(1)
	mockLoader.EXPECT().Get(expectedPrincipalAddress).Return(types.Account{}, nil).Times(1)

	// spawn should call Update to store the newly-spawned account state
	mockUpdater.EXPECT().Update(gomock.Any()).DoAndReturn(func(account types.Account) error {
		require.Equal(t, expectedPrincipalAddress, account.Address)
		require.Equal(t, expectedWalletState, account.State)
		require.Equal(t, templateAddress, *account.TemplateAddress)
		return nil
	}).Times(1)

	// point to the library path
	os.Setenv("ATHENA_LIB_PATH", "../../../build")
	vmLib, err := athcon.LoadLibrary(host.AthenaLibPath())
	require.NoError(t, err)

	athenaPayload := vmLib.EncodeTxSpawn(athcon.Bytes32(pubkey))
	executionPayload := athcon.EncodedExecutionPayload([]byte{}, athenaPayload)

	// Execute the spawn and catch the result
	output, gasLeft, err := (&handler{}).Exec(mockHost, mockLoader, mockUpdater, executionPayload)
	require.Less(t, gasLeft, int64(5000))
	require.Equal(t, expectedPrincipalAddress, types.Address(output))
	require.NoError(t, err)
}

func TestVerify(t *testing.T) {
	const PRIVKEY = "2375b169ab93821366eb5e6898145ec12b6419536b8ee0615cae783b4bc015e7ba216991978cab901254e8eaa062830bbe42c6fc7f56032cbed0e8926ad43e97"
	const PUBKEY = "ba216991978cab901254e8eaa062830bbe42c6fc7f56032cbed0e8926ad43e97"

	// as in Spawn test, above
	const WALLET_STATE = "00000000000000000000000000000000BA216991978CAB901254E8EAA062830BBE42C6FC7F56032CBED0E8926AD43E97"

	walletState, err := hex.DecodeString(WALLET_STATE)
	require.NoError(t, err)
	privkeyBytes, err := hex.DecodeString(PRIVKEY)
	require.NoError(t, err)
	pubkeyBytes, err := hex.DecodeString(PUBKEY)
	require.NoError(t, err)

	require.Equal(t, ed25519.PrivateKey(privkeyBytes).Public().(ed25519.PublicKey), ed25519.PublicKey(pubkeyBytes))

	ctrl := gomock.NewController(t)
	mockHost := mocks.NewMockHost(ctrl)
	mockLoader := mocks.NewMockAccountLoader(ctrl)

	spawnPayload, _ := athcon.FromString("athexp_spawn")

	// Times counts the total number of times these methods are called.
	// Note that wallet.Verify() short-circuits when called on empty input, so it only actually
	// runs twice.
	mockHost.EXPECT().Layer().Return(core.LayerID(1)).Times(2)
	mockHost.EXPECT().Principal().Return(types.Address{2}).Times(5)
	mockHost.EXPECT().MaxGas().Return(100000000).Times(2)
	mockHost.EXPECT().TemplateAddress().Return(types.Address{1}).Times(1)

	// for now, don't include GenesisID
	// empty := types.Hash20{}
	// mockHost.EXPECT().GetGenesisID().Return(empty).Times(3)

	mockTemplate := types.Account{
		State: walletTemplate.PROGRAM,
	}
	mockWallet := types.Account{
		State: walletState,
	}
	mockLoader.EXPECT().Get(types.Address{1}).Return(mockTemplate, nil).Times(1)
	mockLoader.EXPECT().Get(types.Address{2}).Return(mockWallet, nil).Times(1)

	// point to the library path
	os.Setenv("ATHENA_LIB_PATH", "../../../build")

	wallet, err := New(mockHost, mockLoader, append(spawnPayload[:], pubkeyBytes...))
	require.NoError(t, err)

	t.Run("Invalid", func(t *testing.T) {
		buf64 := types.EdSignature{}
		require.False(t, wallet.Verify(mockHost, buf64[:], scale.NewDecoder(bytes.NewReader(buf64[:]))))
	})
	t.Run("Empty", func(t *testing.T) {
		require.False(t, wallet.Verify(mockHost, nil, scale.NewDecoder(bytes.NewBuffer(nil))))
	})
	t.Run("Valid", func(t *testing.T) {
		msg := []byte{1, 2, 3}
		// body := core.SigningBody(empty[:], msg)
		sig := ed25519.Sign(privkeyBytes, msg)
		require.True(
			t,
			wallet.Verify(mockHost, append(msg, sig...), scale.NewDecoder(bytes.NewReader(sig))),
		)
	})
}
