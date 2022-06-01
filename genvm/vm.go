package genvm

import (
	"bytes"
	"context"
	"errors"
	"fmt"

	"github.com/spacemeshos/go-scale"

	"github.com/spacemeshos/go-spacemesh/genvm/core"
	"github.com/spacemeshos/go-spacemesh/genvm/registry"
	_ "github.com/spacemeshos/go-spacemesh/genvm/templates/wallet"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/sql"
)

type Opt func(*VM)

func WithLogger(logger log.Log) Opt {
	return func(vm *VM) {
		vm.logger = logger
	}
}

func New(db *sql.Database, opts ...Opt) *VM {
	vm := &VM{
		logger: log.NewNop(),
		db:     db,
	}
	for _, opt := range opts {
		opt(vm)
	}
	return vm
}

type VM struct {
	logger log.Log
	db     *sql.Database
}

// Validation initializes validation request.
func (vm *VM) Validation(raw []byte) *Request {
	return &Request{
		vm:      vm,
		decoder: scale.NewDecoder(bytes.NewReader(raw)),
		raw:     raw,
	}
}

// Apply transactions.
func (vm *VM) Apply(txs [][]byte) ([][]byte, error) {
	tx, err := vm.db.Tx(context.Background())
	if err != nil {
		return nil, err
	}
	defer tx.Release()
	var (
		ss      = core.NewStagedState(tx)
		rd      bytes.Reader
		decoder = scale.NewDecoder(&rd)
		failed  [][]byte
	)
	for _, tx := range txs {
		rd.Reset(tx)
		_, ctx, args, err := parse(ss, decoder)
		if err != nil {
			failed = append(failed, tx)
		}
		ctx.Handler.Exec(ctx, ctx.Method, args)
		if err := ctx.Apply(ss); err != nil {
			return nil, err
		}
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return failed, nil
}

type Request struct {
	vm *VM

	raw     []byte
	ctx     *core.Context
	decoder *scale.Decoder
}

func (r *Request) Parse() (*core.Header, error) {
	header, ctx, _, err := parse(core.NewStagedState(r.vm.db), r.decoder)
	if err != nil {
		return nil, err
	}
	r.ctx = ctx
	return header, nil
}

func (r *Request) Verify() bool {
	if r.ctx == nil {
		panic("Verify should be called after Parse")
	}
	return verify(r.ctx, r.raw)
}

func parse(loader core.AccountLoader, decoder *scale.Decoder) (*core.Header, *core.Context, any, error) {
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
	account, err := loader.Get(principal)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("failed to load stte for principal %s: %w", principal, err)
	}

	if method == 0 {
		var template scale.Address
		if _, err := template.DecodeScale(decoder); err != nil {
			return nil, nil, nil, fmt.Errorf("failed to decode template address %w", err)
		}
		account.Template = &template
	}
	if account.Template == nil {
		return nil, nil, nil, errors.New("account should be spawned first")
	}

	handler := registry.Get(*account.Template)
	if handler == nil {
		return nil, nil, nil, errors.New("unknown template")
	}
	ctx := &core.Context{
		Loader:    loader,
		Handler:   handler,
		Account:   account,
		Principal: principal,
		Method:    method,
	}
	header, args := handler.Parse(ctx, method, decoder)
	ctx.Template, err = handler.Init(method, args, account.State)
	if err != nil {
		return nil, nil, nil, err
	}
	ctx.Args = args
	ctx.Header = header
	return &header, ctx, args, nil
}

func verify(ctx *core.Context, raw []byte) bool {
	return ctx.Template.Verify(ctx, raw)
}
