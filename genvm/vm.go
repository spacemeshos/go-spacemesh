package vm

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/spacemeshos/go-scale"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/genvm/core"
	"github.com/spacemeshos/go-spacemesh/genvm/registry"
	_ "github.com/spacemeshos/go-spacemesh/genvm/templates/wallet"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/sql"
	"github.com/spacemeshos/go-spacemesh/sql/accounts"
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

func (vm *VM) GetNonce(address core.Address) (core.Nonce, error) {
	account, err := accounts.Latest(vm.db, types.Address(address))
	if err != nil {
		return core.Nonce{}, err
	}
	return core.Nonce{Counter: account.NextNonce()}, nil
}

func (vm *VM) ApplyGenesis(genesis []types.Account) error {
	tx, err := vm.db.Tx(context.Background())
	if err != nil {
		return err
	}
	for i := range genesis {
		account := &genesis[i]
		vm.logger.With().Info("genesis account", log.Inline(account))
		if err := accounts.Update(tx, account); err != nil {
			return fmt.Errorf("inserting genesis account %w", err)
		}
	}
	defer tx.Release()
	return tx.Commit()
}

// Apply transactions.
func (vm *VM) Apply(lid types.LayerID, txs [][]byte) ([][]byte, error) {
	tx, err := vm.db.Tx(context.Background())
	if err != nil {
		return nil, err
	}
	defer tx.Release()
	var (
		ss      = core.NewStagedState(tx)
		rd      bytes.Reader
		decoder = scale.NewDecoder(&rd)
		skipped [][]byte
		start   = time.Now()
	)
	for _, tx := range txs {
		rd.Reset(tx)
		_, ctx, args, err := parse(vm.logger, ss, decoder)
		if err != nil {
			vm.logger.With().Warning("skipping transaction", log.Err(err))
			skipped = append(skipped, tx)
			continue
		}
		ctx.Handler.Exec(ctx, ctx.Method, args)
		if err := ctx.Apply(ss); err != nil {
			return nil, err
		}
	}
	ss.IterateChanged(func(account *core.Account) bool {
		account.Layer = lid
		vm.logger.With().Debug("update account state", log.Inline(account))
		err = accounts.Update(tx, account)
		return err == nil
	})
	if err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	vm.logger.With().Info("applied transactions", lid,
		log.Int("count", len(txs)-len(skipped)),
		log.Duration("duration", time.Since(start)),
	)
	return skipped, nil
}

type Request struct {
	vm *VM

	raw     []byte
	ctx     *core.Context
	decoder *scale.Decoder
}

func (r *Request) Parse() (*core.Header, error) {
	header, ctx, _, err := parse(r.vm.logger, core.NewStagedState(r.vm.db), r.decoder)
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

func parse(logger log.Log, loader core.AccountLoader, decoder *scale.Decoder) (*core.Header, *core.Context, any, error) {
	version, _, err := scale.DecodeCompact8(decoder)
	if err != nil {
		return nil, nil, nil, err
	}
	if version != 0 {
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
		return nil, nil, nil, fmt.Errorf("failed to load state for principal %s: %w", principal, err)
	}
	logger.With().Debug("loaded account state", log.Inline(&account))

	if method == 0 {
		var template scale.Address
		if _, err := template.DecodeScale(decoder); err != nil {
			return nil, nil, nil, fmt.Errorf("failed to decode template address %w", err)
		}
		account.Template = &template
	}
	if account.Template == nil {
		return nil, nil, nil, errors.New("account is not spawned")
	}

	handler := registry.Get(*account.Template)
	if handler == nil {
		return nil, nil, nil, fmt.Errorf("unknown template %x", *account.Template)
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
	header.Principal = principal
	ctx.Args = args
	ctx.Header = header
	return &header, ctx, args, nil
}

func verify(ctx *core.Context, raw []byte) bool {
	return ctx.Template.Verify(ctx, raw)
}
