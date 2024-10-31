package host

import (
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"runtime"

	athcon "github.com/athenavm/athena/ffi/athcon/bindings/go"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/vm/core"

	gossamerScale "github.com/ChainSafe/gossamer/pkg/scale"
)

func AthenaLibPath() string {
	var err error

	constructPath := func(path string) string {
		switch runtime.GOOS {
		case "windows":
			return filepath.Join(path, "../build/libathenavmwrapper.dll")
		case "darwin":
			return filepath.Join(path, "../build/libathenavmwrapper.dylib")
		default:
			return filepath.Join(path, "../build/libathenavmwrapper.so")
		}
	}

	// check first for an env var
	if path := os.Getenv("ATHENA_LIB_PATH"); path != "" {
		return constructPath(path)
	}

	cwd, err := os.Getwd()
	if err != nil {
		log.Fatalf("Failed to get current working directory: %v", err)
	}
	return constructPath(cwd)
}

//go:generate mockgen -typed -package=mocks -destination=./mocks/host.go github.com/spacemeshos/go-spacemesh/vm/core Host

type Host struct {
	vm             *athcon.VM
	host           core.Host
	staticContext  core.StaticContext
	dynamicContext core.DynamicContext
}

// Load the VM from the shared library and returns an instance of a Host.
// It is the caller's responsibility to call Destroy when it
// is no longer needed.
func NewHost(host core.Host) (*Host, error) {
	vm, err := athcon.Load(AthenaLibPath())
	if err != nil {
		return nil, fmt.Errorf("loading Athena VM: %w", err)
	}

	// Construct the context
	staticContext := core.StaticContext{
		// Athena does not currently allow proxied calls, so by definition the principal is the
		// same as the destination, for now. See https://github.com/athenavm/athena/issues/174.
		Principal:   host.Principal(),
		Destination: host.Principal(),
		Nonce:       host.Nonce(),
	}
	dynamicContext := core.DynamicContext{
		Template: host.TemplateAddress(),
		Callee:   host.Principal(),
	}

	return &Host{vm, host, staticContext, dynamicContext}, nil
}

func (h *Host) Destroy() {
	h.vm.Destroy()
}

func (h *Host) Execute(
	layer types.LayerID,
	gas int64,
	recipient, sender types.Address,
	input []byte,
	value uint64,
	code []byte,
) (output []byte, gasLeft int64, err error) {
	hostCtx := &hostContext{
		layer,
		h.host,
		h.staticContext,
		h.dynamicContext,
		h.vm,
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
		value,
		code,
	)
	if err != nil {
		return nil, 0, err
	}

	return r.Output, r.GasLeft, nil
}

type hostContext struct {
	layer          types.LayerID
	host           core.Host
	staticContext  core.StaticContext
	dynamicContext core.DynamicContext
	vm             *athcon.VM
}

var _ athcon.HostContext = (*hostContext)(nil)

func (h *hostContext) AccountExists(addr athcon.Address) bool {
	if has, err := h.host.Has(types.Address(addr)); !has || err != nil {
		return false
	}
	return true
}

func (h *hostContext) GetStorage(addr athcon.Address, key athcon.Bytes32) athcon.Bytes32 {
	if account, err := h.host.Get(types.Address(addr)); err == nil {
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
	status, _ := h.host.SetStorage(types.Address(addr), [32]byte(key), [32]byte(value))
	switch status {
	case core.StorageStatusAdded:
		return athcon.StorageAdded
	case core.StorageStatusModified:
		return athcon.StorageModified
	default:
		panic("unexpected storage status")
	}
}

func (h *hostContext) GetBalance(addr athcon.Address) uint64 {
	if account, err := h.host.Get(types.Address(addr)); err == nil {
		return account.Balance
	}
	return 0
}

func (h *hostContext) GetTxContext() athcon.TxContext {
	// TODO: implement
	return athcon.TxContext{
		GasPrice:    0,
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
	value uint64,
	input []byte,
	gas int64,
	depth int,
) (output []byte, gasLeft int64, err error) {
	// check call depth
	if depth > 10 {
		return nil, 0, athcon.CallDepthExceeded
	}

	// take snapshot of state
	// TODO: implement me

	destinationAccount, err := h.host.Get(types.Address(recipient))
	if err != nil {
		return nil, 0, athcon.Error{
			Code: athcon.InternalError.Code,
			Err:  fmt.Errorf("loading recipient account: %w", err),
		}
	}

	// decode input payload
	var payload athcon.Payload
	if err = gossamerScale.Unmarshal(input, &payload); err != nil {
		return nil, 0, athcon.Error{
			Code: athcon.InternalError.Code,
			Err:  fmt.Errorf("decoding input payload: %w", err),
		}
	}

	// if there is input data, then the destination account must exist and must be spawned
	template := destinationAccount.TemplateAddress
	state := destinationAccount.State
	var templateAccount *types.Account
	if len(payload.Input) > 0 {
		if template == nil || len(state) == 0 {
			return nil, 0, athcon.Error{
				Code: athcon.InternalError.Code,
				Err:  errors.New("missing template information"),
			}
		}

		// read template code
		acct, err := h.host.Get(types.Address(*template))
		if err != nil || len(templateAccount.State) == 0 {
			return nil, 0, athcon.Error{
				Code: athcon.InternalError.Code,
				Err:  fmt.Errorf("loading template account: %w", err),
			}
		}
		templateAccount = &acct
	}

	// balance transfer
	// this does not depend upon the recipient account status
	if err = h.host.Transfer(types.Address(recipient), value); err != nil {
		return nil, 0, athcon.Error{
			Code: athcon.InternalError.Code,
			Err:  fmt.Errorf("balance transfer failed: %w", err),
		}
	}

	if len(payload.Input) == 0 {
		// short-circuit and return if this is a simple balance transfer
		return nil, gas, nil
	}

	// enrich the message with the method selector and account state, then execute the call.
	// note: we skip this step if there's no input (i.e., this is a simple balance transfer).
	input = athcon.EncodedExecutionPayload([]byte{}, input)

	// construct and save context
	oldContext := h.dynamicContext
	h.dynamicContext = core.DynamicContext{
		Template: types.Address(sender),
		Callee:   types.Address(recipient),
	}

	// replace context at end
	defer func() {
		h.dynamicContext = oldContext
	}()

	// execute the call
	res, err := h.vm.Execute(
		h,
		athcon.Frontier,
		kind,
		depth+1,
		gas,
		recipient,
		sender,
		input,
		value,
		templateAccount.State,
	)
	if err != nil {
		// rollback in case of failure/revert
		// TODO: implement me
		// rollback balance transfer
		// rollback storage changes

		return nil, 0, err
	}
	return res.Output, res.GasLeft, nil
}

func (h *hostContext) Deploy(blob []byte) athcon.Address {
	panic("not implemented")
}

func (h *hostContext) Spawn(blob []byte) athcon.Address {
	emptyAddress := types.Address{}

	// make sure we have the required context
	if h.staticContext.Principal == emptyAddress {
		// staticContext is unused today, but will be needed to read principal in the future.
		// see https://github.com/spacemeshos/go-spacemesh/issues/6420
		return athcon.Address(emptyAddress)
	}
	if h.dynamicContext.Template == emptyAddress {
		return athcon.Address(emptyAddress)
	}

	if address, err := h.host.Spawn(types.Address(h.dynamicContext.Template), blob); err != nil {
		return athcon.Address(emptyAddress)
	} else {
		return athcon.Address(address)
	}
}
