package genvm

import (
	"bytes"
	"errors"
	"fmt"

	"github.com/spacemeshos/go-scale"
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/genvm/core"
	_ "github.com/spacemeshos/go-spacemesh/genvm/wallet"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/sql"
	"github.com/spacemeshos/go-spacemesh/sql/accounts"
)

func New(logger *log.Log, db *sql.Database) *VM {
	return &VM{
		logger: logger,
		db:     db,
	}
}

type VM struct {
	logger *log.Log
	db     *sql.Database
}

type Request struct {
	vm *VM

	raw     []byte
	ctx     *core.Context
	decoder *scale.Decoder
}

func (r *Request) Parse() (*core.Header, error) {
	header, ctx, _, err := r.vm.parse(r.decoder)
	if err != nil {
		return nil, err
	}
	r.ctx = ctx
	return header, nil
}

func (r *Request) Verify() bool {
	return r.vm.verify(r.ctx, r.raw)
}

func (vm *VM) Validation(raw []byte) *Request {
	return &Request{
		decoder: scale.NewDecoder(bytes.NewReader(raw)),
		raw:     raw,
	}
}

func (vm *VM) parse(decoder *scale.Decoder) (*core.Header, *core.Context, any, error) {
	version, _, err := scale.DecodeCompact8(decoder)
	if err != nil {
		return nil, nil, nil, err
	}
	if version != 1 {
		return nil, nil, nil, fmt.Errorf("unsupported version %d", version)
	}

	var principal scale.Address
	if _, err := principal.DecodeScale(decoder); err != nil {
		return nil, nil, nil, fmt.Errorf("failed to decode principal: %w", err)
	}
	method, _, err := scale.DecodeCompact8(decoder)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("failed to decode method selector: %w", err)
	}
	state, err := accounts.Latest(vm.db, types.Address(principal))
	if err != nil {
		return nil, nil, nil, fmt.Errorf("failed to load stte for principal %s: %w", principal, err)
	}
	if method == 0 {
		var template scale.Address
		if _, err := template.DecodeScale(decoder); err != nil {
			return nil, nil, nil, fmt.Errorf("failed to decode template address %w", err)
		}
		state.Template = &template
	}
	if state.Template == nil {
		return nil, nil, nil, errors.New("account should be spawned first")
	}
	handler := core.Get(*state.Template)
	if handler == nil {
		return nil, nil, nil, errors.New("unknown template")
	}
	ctx := &core.Context{
		Handler:   handler,
		State:     &state,
		Principal: principal,
		Method:    method,
	}
	header, args := handler.Parse(ctx, method, decoder)
	ctx.Template = handler.Load(ctx, method, &header)
	return &header, ctx, args, nil
}

func (vm *VM) verify(ctx *core.Context, raw []byte) bool {
	return ctx.Template.Verify(ctx, raw)
}

// Apply transaction. Returns true if transaction run out of gas in the validation phase.
func (vm *VM) Apply(txs [][]byte) [][]byte {
	var (
		changes []*core.Account
		rd      bytes.Reader
		decoder = scale.NewDecoder(&rd)
		failed  [][]byte
	)
	for _, tx := range txs {
		rd.Reset(tx)
		_, ctx, args, err := vm.parse(decoder)
		if err != nil {
			failed = append(failed, tx)
		}
		// TODO skip verification but consume cost
		ctx.Consume(100)
		ctx.Handler.Exec(ctx, ctx.Method, args)
		changes = append(changes, ctx.State)
	}
	return failed
}
