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

// Opt is for changing VM during initialization.
type Opt func(*VM)

// WithLogget sets logger for VM.
func WithLogger(logger log.Log) Opt {
	return func(vm *VM) {
		vm.logger = logger
	}
}

// New returns VM instance.
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

// VM handles modifications to the account state.
type VM struct {
	logger log.Log
	db     *sql.Database
}

// Validation initializes validation request.
func (vm *VM) Validation(raw types.RawTx) *Request {
	return &Request{
		vm:      vm,
		decoder: scale.NewDecoder(bytes.NewReader(raw.Raw)),
		raw:     raw,
	}
}

// GetNonce returns expected next nonce for the address.
func (vm *VM) GetNonce(address core.Address) (core.Nonce, error) {
	account, err := accounts.Latest(vm.db, types.Address(address))
	if err != nil {
		return core.Nonce{}, err
	}
	return core.Nonce{Counter: account.NextNonce()}, nil
}

// ApplyGenesis saves list of accounts for genesis.
func (vm *VM) ApplyGenesis(genesis []types.Account) error {
	tx, err := vm.db.Tx(context.Background())
	if err != nil {
		return err
	}
	defer tx.Release()
	for i := range genesis {
		account := &genesis[i]
		vm.logger.With().Info("genesis account", log.Inline(account))
		if err := accounts.Update(tx, account); err != nil {
			return fmt.Errorf("inserting genesis account %w", err)
		}
	}
	return tx.Commit()
}

// Apply transactions.
func (vm *VM) Apply(lid types.LayerID, txs []types.RawTx) ([]types.TransactionID, error) {
	tx, err := vm.db.Tx(context.Background())
	if err != nil {
		return nil, err
	}
	defer tx.Release()
	var (
		ss      = core.NewStagedCache(tx)
		rd      bytes.Reader
		decoder = scale.NewDecoder(&rd)
		skipped []types.TransactionID
		start   = time.Now()
	)
	for i := range txs {
		tx := &txs[i]
		rd.Reset(tx.Raw)
		header, ctx, args, err := parse(vm.logger, ss, tx.ID, decoder)
		if err != nil {
			vm.logger.With().Warning("skipping transaction. failed to parse", log.Err(err))
			skipped = append(skipped, tx.ID)
			continue
		}
		vm.logger.With().Debug("applying transaction",
			log.Object("header", header),
			log.Object("account", &ctx.Account),
		)
		if ctx.Account.Balance < ctx.Header.MaxGas*ctx.Header.GasPrice {
			vm.logger.With().Warning("skipping transaction. can't cover gas",
				log.Object("header", header),
				log.Object("account", &ctx.Account),
			)
			skipped = append(skipped, tx.ID)
			continue
		}
		if ctx.Account.NextNonce() != ctx.Header.Nonce.Counter {
			vm.logger.With().Warning("skipping transaction. failed nonce check",
				log.Object("header", header),
				log.Object("account", &ctx.Account),
			)
			skipped = append(skipped, tx.ID)
			continue
		}
		if err := ctx.Handler.Exec(ctx, ctx.Header.Method, args); err != nil {
			vm.logger.With().Debug("transaction failed",
				log.Object("header", header),
				log.Object("account", &ctx.Account),
				log.Err(err),
			)
			if errors.Is(err, core.ErrInternal) {
				return nil, err
			}
		}
		if err := ctx.Apply(ss); err != nil {
			return nil, fmt.Errorf("%w: %s", core.ErrInternal, err.Error())
		}
	}
	// TODO move rewards here
	ss.IterateChanged(func(account *core.Account) bool {
		account.Layer = lid
		vm.logger.With().Debug("update account state", log.Inline(account))
		err = accounts.Update(tx, account)
		return err == nil
	})
	if err != nil {
		return nil, fmt.Errorf("%w: %s", core.ErrInternal, err.Error())
	}
	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("%w: %s", core.ErrInternal, err.Error())
	}
	vm.logger.With().Info("applied transactions", lid,
		log.Int("count", len(txs)-len(skipped)),
		log.Duration("duration", time.Since(start)),
	)
	return skipped, nil
}

// Request used to implement 2-step validation flow.
// After Parse is executed - conservative cache may do validation and skip Verify
// if transaction can't be executed.
type Request struct {
	vm *VM

	raw     types.RawTx
	ctx     *core.Context
	decoder *scale.Decoder
}

// Parse header from the raw transaction.
func (r *Request) Parse() (*core.Header, error) {
	header, ctx, _, err := parse(r.vm.logger, core.NewStagedCache(r.vm.db), r.raw.ID, r.decoder)
	if err != nil {
		return nil, err
	}
	r.ctx = ctx
	return header, nil
}

// Verify transaction. Will panic if called without Parse completing succesfully.
func (r *Request) Verify() bool {
	if r.ctx == nil {
		panic("Verify should be called after succesfull Parse")
	}
	return verify(r.ctx, r.raw.Raw)
}

func parse(logger log.Log, loader core.AccountLoader, id types.TransactionID, decoder *scale.Decoder) (*core.Header, *core.Context, scale.Encodable, error) {
	version, _, err := scale.DecodeCompact8(decoder)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("%w: failed to decode version %s", core.ErrMalformed, err.Error())
	}
	if version != 0 {
		return nil, nil, nil, fmt.Errorf("%w: unsupported version %d", core.ErrMalformed, version)
	}

	var principal core.Address
	if _, err := principal.DecodeScale(decoder); err != nil {
		return nil, nil, nil, fmt.Errorf("%w failed to decode principal: %s", core.ErrMalformed, err)
	}
	method, _, err := scale.DecodeCompact8(decoder)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("%w: failed to decode method selector %s", core.ErrMalformed, err.Error())
	}
	account, err := loader.Get(principal)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("%w: failed load state for principal %s - %s", core.ErrInternal, principal, err)
	}
	logger.With().Debug("loaded account state", log.Inline(&account))

	var template *core.Address
	if method == 0 {
		template = &core.Address{}
		if _, err := template.DecodeScale(decoder); err != nil {
			return nil, nil, nil, fmt.Errorf("%w failed to decode template address %s", core.ErrMalformed, err)
		}
	} else {
		template = account.Template
	}
	if template == nil {
		return nil, nil, nil, fmt.Errorf("%w: %s", core.ErrNotSpawned, principal)
	}

	handler := registry.Get(*template)
	if handler == nil {
		return nil, nil, nil, fmt.Errorf("%w: unknown template %s", core.ErrMalformed, *account.Template)
	}
	ctx := &core.Context{
		Loader:  loader,
		Handler: handler,
		Account: account,
	}
	header, args, err := handler.Parse(ctx, method, decoder)
	if err != nil {
		return nil, nil, nil, err
	}
	header.ID = id
	header.Principal = principal
	header.Template = *template
	header.Method = method

	ctx.Args = args
	ctx.Header = header

	ctx.Template, err = handler.Init(method, args, account.State)
	if err != nil {
		return nil, nil, nil, err
	}
	maxspend, err := ctx.Template.MaxSpend(ctx.Header.Method, args)
	if err != nil {
		return nil, nil, nil, err
	}
	ctx.Header.MaxSpend = maxspend
	return &ctx.Header, ctx, args, nil
}

func verify(ctx *core.Context, raw []byte) bool {
	return ctx.Template.Verify(ctx, raw)
}
