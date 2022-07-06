package vm

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/spacemeshos/go-scale"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/common/types/address"
	"github.com/spacemeshos/go-spacemesh/events"
	"github.com/spacemeshos/go-spacemesh/genvm/core"
	"github.com/spacemeshos/go-spacemesh/genvm/registry"
	"github.com/spacemeshos/go-spacemesh/genvm/templates/wallet"
	"github.com/spacemeshos/go-spacemesh/hash"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/sql"
	"github.com/spacemeshos/go-spacemesh/sql/accounts"
	"github.com/spacemeshos/go-spacemesh/sql/layers"
	"github.com/spacemeshos/go-spacemesh/sql/rewards"
	"github.com/spacemeshos/go-spacemesh/sql/transactions"
	"github.com/spacemeshos/go-spacemesh/system"
)

// Opt is for changing VM during initialization.
type Opt func(*VM)

// WithLogger sets logger for VM.
func WithLogger(logger log.Log) Opt {
	return func(vm *VM) {
		vm.logger = logger
	}
}

// New returns VM instance.
func New(db *sql.Database, opts ...Opt) *VM {
	vm := &VM{
		logger:   log.NewNop(),
		db:       db,
		cfg:      DefaultRewardConfig(),
		registry: registry.New(),
	}
	wallet.Register(vm.registry)
	for _, opt := range opts {
		opt(vm)
	}
	return vm
}

// VM handles modifications to the account state.
type VM struct {
	logger   log.Log
	db       *sql.Database
	cfg      RewardConfig
	registry *registry.Registry
}

// Validation initializes validation request.
func (v *VM) Validation(raw types.RawTx) system.ValidationRequest {
	return &Request{
		vm:      v,
		decoder: scale.NewDecoder(bytes.NewReader(raw.Raw)),
		raw:     raw,
	}
}

// GetLayerStateRoot returns the state root at a given layer.
func (v *VM) GetLayerStateRoot(lid types.LayerID) (types.Hash32, error) {
	return layers.GetStateHash(v.db, lid)
}

// GetLayerApplied returns layer of the applied transaction.
func (v *VM) GetLayerApplied(tid types.TransactionID) (types.LayerID, error) {
	return transactions.GetAppliedLayer(v.db, tid)
}

// GetStateRoot gets the current state root hash.
func (v *VM) GetStateRoot() (types.Hash32, error) {
	return layers.GetLatestStateHash(v.db)
}

// GetAllAccounts returns a dump of all accounts in global state.
func (v *VM) GetAllAccounts() ([]*types.Account, error) {
	return accounts.All(v.db)
}

func (v *VM) revert(lid types.LayerID) error {
	tx, err := v.db.Tx(context.Background())
	if err != nil {
		return err
	}
	defer tx.Release()

	err = accounts.Revert(tx, lid)
	if err != nil {
		return err
	}
	err = rewards.Revert(tx, lid)
	if err != nil {
		return err
	}
	return tx.Commit()
}

// Revert all changes that we made after the layer. Returns state hash of the layer.
func (v *VM) Revert(lid types.LayerID) (types.Hash32, error) {
	if err := v.revert(lid); err != nil {
		return types.Hash32{}, err
	}
	return v.GetStateRoot()
}

// AccountExists returns true if the address exists, spawned or not.
func (v *VM) AccountExists(address core.Address) (bool, error) {
	return accounts.Has(v.db, address)
}

// GetNonce returns expected next nonce for the address.
func (v *VM) GetNonce(address core.Address) (core.Nonce, error) {
	account, err := accounts.Latest(v.db, address)
	if err != nil {
		return core.Nonce{}, err
	}
	return core.Nonce{Counter: account.NextNonce()}, nil
}

// GetBalance returns balance for an address.
func (v *VM) GetBalance(address address.Address) (uint64, error) {
	account, err := accounts.Latest(v.db, address)
	if err != nil {
		return 0, err
	}
	return account.Balance, nil
}

// ApplyGenesis saves list of accounts for genesis.
func (v *VM) ApplyGenesis(genesis []types.Account) error {
	tx, err := v.db.Tx(context.Background())
	if err != nil {
		return err
	}
	defer tx.Release()
	for i := range genesis {
		account := &genesis[i]
		v.logger.With().Info("genesis account", log.Inline(account))
		if err := accounts.Update(tx, account); err != nil {
			return fmt.Errorf("inserting genesis account %w", err)
		}
	}
	return tx.Commit()
}

// Apply transactions.
func (v *VM) Apply(lctx ApplyContext, txs []types.RawTx, blockRewards []types.AnyReward) ([]types.TransactionID, []types.TransactionWithResult, error) {
	tx, err := v.db.TxImmediate(context.Background())
	if err != nil {
		return nil, nil, err
	}
	defer tx.Release()
	var (
		ss      = core.NewStagedCache(tx)
		rd      bytes.Reader
		decoder = scale.NewDecoder(&rd)
		skipped []types.TransactionID
		start   = time.Now()
		fees    uint64
		results []types.TransactionWithResult
	)
	for i := range txs {
		tx := &txs[i]
		rd.Reset(tx.Raw)
		header, ctx, args, err := parse(v.logger, v.registry, ss, tx.ID, decoder)
		if err != nil {
			v.logger.With().Warning("skipping transaction. failed to parse", log.Err(err))
			skipped = append(skipped, tx.ID)
			continue
		}
		v.logger.With().Debug("applying transaction",
			log.Object("header", header),
			log.Object("account", &ctx.Account),
		)
		if ctx.Account.Balance < ctx.Header.MaxGas*ctx.Header.GasPrice {
			v.logger.With().Warning("skipping transaction. can't cover gas",
				log.Object("header", header),
				log.Object("account", &ctx.Account),
			)
			skipped = append(skipped, tx.ID)
			continue
		}
		if ctx.Account.NextNonce() != ctx.Header.Nonce.Counter {
			v.logger.With().Warning("skipping transaction. failed nonce check",
				log.Object("header", header),
				log.Object("account", &ctx.Account),
			)
			skipped = append(skipped, tx.ID)
			continue
		}
		rst := types.TransactionWithResult{}
		rst.Layer = lctx.Layer
		rst.Block = lctx.Block
		err = ctx.Handler.Exec(ctx, ctx.Header.Method, args)
		if err != nil {
			v.logger.With().Debug("transaction failed",
				log.Object("header", header),
				log.Object("account", &ctx.Account),
				log.Err(err),
			)
			// TODO anything but internal must be recorded in the execution result.
			// internal errors are propagated upwards, but they are for fatal
			// unrecovarable errors, such as disk problems
			if errors.Is(err, core.ErrInternal) {
				return nil, nil, err
			}
		}
		rst.RawTx = txs[i]
		rst.TxHeader = &ctx.Header
		rst.Status = types.TransactionSuccess
		if err != nil {
			rst.Status = types.TransactionFailure
			rst.Message = err.Error()
		}
		rst.Gas = ctx.Consumed()
		rst.Fee = ctx.Fee()
		rst.Addresses = ctx.Updated()

		err = ctx.Apply(ss)
		if err != nil {
			return nil, nil, fmt.Errorf("%w: %s", core.ErrInternal, err.Error())
		}
		fees += ctx.Fee()
		results = append(results, rst)
	}

	// TODO(dshulyak) why it fails if there are no rewards?
	if len(blockRewards) > 0 {
		finalRewards, err := calculateRewards(v.logger, v.cfg, lctx.Layer, fees, blockRewards)
		if err != nil {
			return nil, nil, fmt.Errorf("%w: %s", core.ErrInternal, err.Error())
		}
		for _, reward := range finalRewards {
			if err := rewards.Add(tx, reward); err != nil {
				return nil, nil, fmt.Errorf("%w: %s", core.ErrInternal, err.Error())
			}
			account, err := ss.Get(reward.Coinbase)
			if err != nil {
				return nil, nil, fmt.Errorf("%w: %s", core.ErrInternal, err.Error())
			}
			account.Balance += reward.TotalReward
			if err := ss.Update(account); err != nil {
				return nil, nil, fmt.Errorf("%w: %s", core.ErrInternal, err.Error())
			}
		}
	}

	hasher := hash.New()
	encoder := scale.NewEncoder(hasher)
	ss.IterateChanged(func(account *core.Account) bool {
		account.Layer = lctx.Layer
		v.logger.With().Debug("update account state", log.Inline(account))
		err = accounts.Update(tx, account)
		if err != nil {
			return false
		}
		account.EncodeScale(encoder)
		events.ReportAccountUpdate(account.Address)
		return true
	})
	if err != nil {
		return nil, nil, fmt.Errorf("%w: %s", core.ErrInternal, err.Error())
	}
	var hash types.Hash32
	hasher.Sum(hash[:0])
	if err := layers.UpdateStateHash(tx, lctx.Layer, hash); err != nil {
		return nil, nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, nil, fmt.Errorf("%w: %s", core.ErrInternal, err.Error())
	}
	v.logger.With().Info("applied transactions",
		lctx.Layer,
		lctx.Block,
		log.Int("count", len(txs)-len(skipped)),
		log.Duration("duration", time.Since(start)),
		log.Stringer("state_hash", hash),
	)
	return skipped, results, nil
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
	header, ctx, _, err := parse(r.vm.logger, r.vm.registry, core.NewStagedCache(r.vm.db), r.raw.ID, r.decoder)
	if err != nil {
		return nil, err
	}
	r.ctx = ctx
	return header, nil
}

// Verify transaction. Will panic if called without Parse completing successfully.
func (r *Request) Verify() bool {
	if r.ctx == nil {
		panic("Verify should be called after successful Parse")
	}
	return verify(r.ctx, r.raw.Raw)
}

func parse(logger log.Log, reg *registry.Registry, loader core.AccountLoader, id types.TransactionID, decoder *scale.Decoder) (*core.Header, *core.Context, scale.Encodable, error) {
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
		return nil, nil, nil, fmt.Errorf("%w: %s", core.ErrNotSpawned, principal.String())
	}

	handler := reg.Get(*template)
	if handler == nil {
		return nil, nil, nil, fmt.Errorf("%w: unknown template %s", core.ErrMalformed, *template)
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

// ApplyContext has information on layer and block id.
type ApplyContext struct {
	Layer types.LayerID
	Block types.BlockID
}
