package wallet

import (
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

func TestSpawn(t *testing.T) {
	ctrl := gomock.NewController(t)
	mockHost := mocks.NewMockHost(ctrl)

	pubkey, _, err := ed25519.GenerateKey(nil)
	require.NoError(t, err)
	principalAddress := core.ComputePrincipalFromBlob(TemplateAddress, pubkey)

	const maxGas = 100_000
	mockHost.EXPECT().Principal().Return(principalAddress).Times(2)
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
