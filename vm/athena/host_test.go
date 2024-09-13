package vm

import (
	"encoding/binary"
	"log"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	athcon "github.com/athenavm/athena/ffi/athcon/bindings/go"
	"github.com/stretchr/testify/require"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/sql/accounts"
	"github.com/spacemeshos/go-spacemesh/sql/statesql"
	"github.com/spacemeshos/go-spacemesh/vm/templates/getbalance"
)

func athenaLibPath() string {
	var err error

	cwd, err := os.Getwd()
	if err != nil {
		log.Fatalf("Failed to get current working directory: %v", err)
	}

	switch runtime.GOOS {
	case "windows":
		return filepath.Join(cwd, "../build/libathenavmwrapper.dll")
	case "darwin":
		return filepath.Join(cwd, "../build/libathenavmwrapper.dylib")
	default:
		return filepath.Join(cwd, "../build/libathenavmwrapper.so")
	}
}

func TestNewHost(t *testing.T) {
	host, err := NewHost(athenaLibPath(), statesql.InMemoryTest(t))
	require.NoError(t, err)
	defer host.Destroy()

	require.Equal(t, "Athena", host.vm.Name())
}

func TestGetBalance(t *testing.T) {
	host, err := NewHost(athenaLibPath(), statesql.InMemoryTest(t))
	require.NoError(t, err)
	defer host.Destroy()

	account := types.Account{
		Layer:   types.LayerID(15),
		Address: types.Address{1, 2, 3, 4},
		Balance: 100,
	}
	err = accounts.Update(host.db, &account)
	require.NoError(t, err)

	out, gasLeft, err := host.Execute(
		account.Layer,
		10000,
		account.Address,
		account.Address,
		nil,
		[32]byte{},
		getbalance.PROGRAM,
	)

	require.NoError(t, err)
	balance := binary.LittleEndian.Uint64(out)
	require.Equal(t, account.Balance, balance)
	require.NotZero(t, gasLeft)
}

func TestNotEnoughGas(t *testing.T) {
	host, err := NewHost(athenaLibPath(), statesql.InMemoryTest(t))
	require.NoError(t, err)
	defer host.Destroy()

	_, gasLeft, err := host.Execute(
		10,
		10,
		types.Address{1, 2, 3, 4},
		types.Address{1, 2, 3, 4},
		nil,
		[32]byte{},
		getbalance.PROGRAM,
	)

	// FIXME: export more errors in athcon
	// 3 is "out of gas"
	require.ErrorIs(t, err, athcon.Error(3))
	require.Zero(t, gasLeft)
}
