package vm

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"runtime"

	athcon "github.com/athenavm/athena/ffi/athcon/bindings/go"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/vm/core"
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

type Host struct {
	vm      *athcon.VM
	host    core.Host
	loader  core.AccountLoader
	updater core.AccountUpdater
}

// Load the VM from the shared library and returns an instance of a Host.
// It is the caller's responsibility to call Destroy when it
// is no longer needed.
func NewHost(path string, host core.Host, loader core.AccountLoader, updater core.AccountUpdater) (*Host, error) {
	vm, err := athcon.Load(path)
	if err != nil {
		return nil, fmt.Errorf("loading Athena VM: %w", err)
	}
	return &Host{vm, host, loader, updater}, nil
}

func (h *Host) Destroy() {
	h.vm.Destroy()
}

func (h *Host) Execute(
	layer types.LayerID,
	gas int64,
	recipient, sender types.Address,
	input []byte,
	method []byte,
	value [32]byte,
	code []byte,
) (output []byte, gasLeft int64, err error) {
	hostCtx := &hostContext{
		layer,
		h.host,
		h.loader,
		h.updater,
	}
	r, err := h.vm.Execute(
		hostCtx,
		athcon.Frontier,
		athcon.Call,
		0,
		gas,
		athcon.Address(recipient),
		athcon.Address(sender),
		input,
		method,
		athcon.Bytes32(value),
		code,
	)
	if err != nil {
		return nil, 0, err
	}

	return r.Output, r.GasLeft, nil
}

type hostContext struct {
	layer   types.LayerID
	host    core.Host
	loader  core.AccountLoader
	updater core.AccountUpdater
}

var _ athcon.HostContext = (*hostContext)(nil)

func (h *hostContext) AccountExists(addr athcon.Address) bool {
	return h.loader.Has(types.Address(addr))
}

func (h *hostContext) GetStorage(addr athcon.Address, key athcon.Bytes32) athcon.Bytes32 {
	if account, err := h.loader.Get(types.Address(addr)); err == nil {
		// TODO(lane): make this more efficient
		for _, item := range account.Storage {
			if item.Key == key {
				return item.Value
			}
		}
	}
	return [32]byte{}
}

func (h *hostContext) SetStorage(
	addr athcon.Address,
	key athcon.Bytes32,
	value athcon.Bytes32,
) athcon.StorageStatus {
	if account, err := h.loader.Get(types.Address(addr)); err == nil {
		// TODO(lane): make this more efficient
		for i, item := range account.Storage {
			if item.Key == key {
				account.Storage[i].Value = value
				_ = h.updater.Update(account)
				return athcon.StorageModified
			}
		}
		account.Storage = append(account.Storage, types.StorageItem{Key: key, Value: value})
		_ = h.updater.Update(account)
		return athcon.StorageAdded
	}
	panic("account not found")
}

func (h *hostContext) GetBalance(addr athcon.Address) uint64 {
	if account, err := h.loader.Get(types.Address(addr)); err == nil {
		return account.Balance
	}
	return 0
}

func (h *hostContext) GetTxContext() athcon.TxContext {
	// TODO: implement
	return athcon.TxContext{
		GasPrice:    [32]byte{},
		Origin:      [24]byte{},
		Coinbase:    [24]byte{},
		BlockHeight: 0,
		Timestamp:   0,
		GasLimit:    0,
		ChainID:     [32]byte{},
	}
}

func (h *hostContext) GetBlockHash(number int64) athcon.Bytes32 {
	panic("not implemented")
}

func (h *hostContext) Call(
	kind athcon.CallKind,
	recipient athcon.Address,
	sender athcon.Address,
	value athcon.Bytes32,
	input []byte,
	method []byte,
	gas int64,
	depth int,
) (output []byte, gasLeft int64, createAddr athcon.Address, err error) {
	panic("not implemented")
}

func (h *hostContext) Deploy(blob []byte) athcon.Address {
	panic("not implemented")
}

func (h *hostContext) Spawn(blob []byte) athcon.Address {
	// make sure the account isn't already spawned

	// create a new account with the code
	panic("unimplemented")
}
