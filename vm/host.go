package vm

import (
	"encoding/binary"
	"fmt"

	athcon "github.com/athenavm/athena/ffi/athcon/bindings/go"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/sql"
	"github.com/spacemeshos/go-spacemesh/sql/accounts"
)

type Host struct {
	vm *athcon.VM
	db sql.Executor
}

// Load the VM from the shared library and returns an instance of a Host.
// It is the caller's responsibility to call Destroy when it
// is no longer needed.
func NewHost(path string, db sql.Executor) (*Host, error) {
	vm, err := athcon.Load(path)
	if err != nil {
		return nil, fmt.Errorf("loading Athena VM: %w", err)
	}
	return &Host{vm, db}, nil
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
		h.db,
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
	layer types.LayerID
	db    sql.Executor
}

var _ athcon.HostContext = (*hostContext)(nil)

func (h *hostContext) AccountExists(addr athcon.Address) bool {
	has, err := accounts.Has(h.db, types.Address(addr))
	return err != nil && has
}

func (h *hostContext) GetStorage(addr athcon.Address, key athcon.Bytes32) athcon.Bytes32 {
	panic("not implemented")
}

func (h *hostContext) SetStorage(
	addr athcon.Address,
	key athcon.Bytes32,
	value athcon.Bytes32,
) athcon.StorageStatus {
	panic("not implemented")
}

func (h *hostContext) GetBalance(addr athcon.Address) athcon.Bytes32 {
	var balance athcon.Bytes32
	if account, err := accounts.Get(h.db, types.Address(addr), h.layer); err == nil {
		binary.LittleEndian.PutUint64(balance[:], account.Balance)
	}
	return balance
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
	panic("unimplemented")
}
