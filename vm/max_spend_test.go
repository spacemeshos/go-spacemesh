package vm

import (
	"encoding/hex"
	"testing"

	athcon "github.com/athenavm/athena/ffi/athcon/bindings/go"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
	"go.uber.org/zap/zaptest"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/vm/core"
	"github.com/spacemeshos/go-spacemesh/vm/core/mocks"
	"github.com/spacemeshos/go-spacemesh/vm/host"
	"github.com/spacemeshos/go-spacemesh/vm/templates/wallet"
)

const WALLET_STATE = "ba216991978cab901254e8eaa062830bbe42c6fc7f56032cbed0e8926ad43e97"

func TestMaxSpend(t *testing.T) {
	const amount = 100
	principalAddress := types.Address{1, 2, 3, 4}
	walletState, err := hex.DecodeString(WALLET_STATE)
	require.NoError(t, err)

	account := types.Account{Address: principalAddress, State: walletState}

	libPath, err := host.AthenaLibPath()
	require.NoError(t, err)
	vmlib, err := athcon.LoadLibrary(libPath)
	require.NoError(t, err)
	defer vmlib.Close()

	t.Run("Spawn", func(t *testing.T) {
		ctrl := gomock.NewController(t)
		mockHost := mocks.NewMockHost(ctrl)
		mockHost.EXPECT().Principal().Return(principalAddress).AnyTimes()
		mockHost.EXPECT().TemplateAddress().Return(wallet.TemplateAddress).AnyTimes()
		mockHost.EXPECT().MaxGas().Return(100000)

		spawnPayload := vmlib.EncodeTxSpawn(athcon.Bytes32{})
		max, err := maxSpend(mockHost, &account, spawnPayload, zaptest.NewLogger(t))
		require.NoError(t, err)
		require.EqualValues(t, 0, max)
	})
	t.Run("Spend", func(t *testing.T) {
		ctrl := gomock.NewController(t)
		mockHost := mocks.NewMockHost(ctrl)
		mockHost.EXPECT().Principal().Return(principalAddress).AnyTimes()
		mockHost.EXPECT().TemplateAddress().Return(wallet.TemplateAddress).AnyTimes()
		mockHost.EXPECT().Template().Return(wallet.PROGRAM)
		mockHost.EXPECT().MaxGas().Return(100000)
		mockHost.EXPECT().Clone().Return(mockHost)
		mockHost.EXPECT().Layer().Return(core.LayerID(1)).AnyTimes()

		spendPayload := vmlib.EncodeTxSpend(athcon.Address{}, amount)
		max, err := maxSpend(mockHost, &account, spendPayload, zaptest.NewLogger(t))
		require.NoError(t, err)
		require.EqualValues(t, amount, max)
	})
}
