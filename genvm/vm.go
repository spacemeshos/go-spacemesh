package vm

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/spacemeshos/go-scale"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/events"
	"github.com/spacemeshos/go-spacemesh/genvm/core"
	"github.com/spacemeshos/go-spacemesh/genvm/registry"
	"github.com/spacemeshos/go-spacemesh/genvm/templates/multisig"
	"github.com/spacemeshos/go-spacemesh/genvm/templates/vault"
	"github.com/spacemeshos/go-spacemesh/genvm/templates/vesting"
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

// Config defines the configuration options for vm.
type Config struct {
	GasLimit  uint64
	GenesisID types.Hash20
}

// DefaultConfig returns the default RewardConfig.
func DefaultConfig() Config {
	return Config{
		GasLimit: 100_000_000,
	}
}

// WithConfig updates config on the vm.
func WithConfig(cfg Config) Opt {
	return func(vm *VM) {
		vm.cfg = cfg
	}
}

// New returns VM instance.
func New(db *sql.Database, opts ...Opt) *VM {
	vm := &VM{
		logger:   log.NewNop(),
		db:       db,
		cfg:      DefaultConfig(),
		registry: registry.New(),
	}
	wallet.Register(vm.registry)
	multisig.Register(vm.registry)
	vesting.Register(vm.registry)
	vault.Register(vm.registry)
	for _, opt := range opts {
		opt(vm)
	}
	return vm
}

// VM handles modifications to the account state.
type VM struct {
	logger   log.Log
	db       *sql.Database
	cfg      Config
	registry *registry.Registry
}

// Validation initializes validation request.
func (v *VM) Validation(raw types.RawTx) system.ValidationRequest {
	return &Request{
		vm:      v,
		cache:   core.NewStagedCache(core.DBLoader{Executor: v.db}),
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
	root, err := layers.GetLatestStateHash(v.db)
	// TODO: reconsider this.
	// instead of skipping vm on empty layers, maybe pass empty layer to vm
	// and let it persist empty (or previous if we will use cumulative) hash.
	if errors.Is(err, sql.ErrNotFound) {
		return types.Hash32{}, nil
	}
	return root, err
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

// Revert all changes that we made after the layer.
func (v *VM) Revert(lid types.LayerID) error {
	if err := v.revert(lid); err != nil {
		return err
	}
	v.logger.With().Info("vm reverted to layer", lid)
	return nil
}

// AccountExists returns true if the address exists, spawned or not.
func (v *VM) AccountExists(address core.Address) (bool, error) {
	return accounts.Has(v.db, address)
}

// GetNonce returns expected next nonce for the address.
func (v *VM) GetNonce(address core.Address) (core.Nonce, error) {
	account, err := accounts.Latest(v.db, address)
	if err != nil {
		return 0, err
	}
	return account.NextNonce, nil
}

// GetBalance returns balance for an address.
func (v *VM) GetBalance(address types.Address) (uint64, error) {
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
			return fmt.Errorf("inserting genesis account: %w", err)
		}
	}
	return tx.Commit()
}

// Apply transactions.
func (v *VM) Apply(lctx ApplyContext, txs []types.Transaction, blockRewards []types.CoinbaseReward) ([]types.Transaction, []types.TransactionWithResult, error) {
	if lctx.Layer.Before(types.GetEffectiveGenesis()) {
		return nil, nil, fmt.Errorf("%w: applying layer %s before effective genesis %s",
			core.ErrInternal, lctx.Layer, types.GetEffectiveGenesis(),
		)
	}
	t1 := time.Now()
	blockDurationWait.Observe(float64(time.Since(t1)))

	ss := core.NewStagedCache(core.DBLoader{Executor: v.db})
	results, skipped, fees, err := v.execute(lctx, ss, txs)
	if err != nil {
		return nil, nil, err
	}
	t2 := time.Now()
	blockDurationTxs.Observe(float64(time.Since(t1)))

	rewardsResult, err := v.addRewards(lctx, ss, fees, blockRewards)
	if err != nil {
		return nil, nil, err
	}

	t3 := time.Now()
	blockDurationRewards.Observe(float64(time.Since(t2)))

	hasher := hash.New()
	encoder := scale.NewEncoder(hasher)
	total := 0

	tx, err := v.db.TxImmediate(context.Background())
	if err != nil {
		return nil, nil, err
	}
	defer tx.Release()

	for _, reward := range rewardsResult {
		if err := rewards.Add(tx, &reward); err != nil {
			return nil, nil, fmt.Errorf("%w: %s", core.ErrInternal, err.Error())
		}
	}

	ss.IterateChanged(func(account *core.Account) bool {
		total++
		account.Layer = lctx.Layer
		v.logger.With().Debug("update account state", log.Inline(account))
		err = accounts.Update(tx, account)
		if err != nil {
			return false
		}
		account.EncodeScale(encoder)
		return true
	})
	if err != nil {
		return nil, nil, fmt.Errorf("%w: %s", core.ErrInternal, err.Error())
	}
	writesPerBlock.Observe(float64(total))

	var hash types.Hash32
	hasher.Sum(hash[:0])
	if err := layers.UpdateStateHash(tx, lctx.Layer, hash); err != nil {
		return nil, nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, nil, fmt.Errorf("%w: %s", core.ErrInternal, err.Error())
	}
	ss.IterateChanged(func(account *core.Account) bool {
		events.ReportAccountUpdate(account.Address)
		return true
	})
	for _, reward := range rewardsResult {
		events.ReportRewardReceived(events.Reward{
			Layer:       reward.Layer,
			Total:       reward.TotalReward,
			LayerReward: reward.LayerReward,
			Coinbase:    reward.Coinbase,
		})
	}

	blockDurationPersist.Observe(float64(time.Since(t3)))
	blockDuration.Observe(float64(time.Since(t1)))
	transactionsPerBlock.Observe(float64(len(txs)))
	appliedLayer.Set(float64(lctx.Layer))

	v.logger.With().Debug("applied layer",
		log.Uint32("layer", lctx.Layer.Uint32()),
		log.Int("count", len(txs)-len(skipped)),
		log.Duration("duration", time.Since(t1)),
		log.Stringer("state_hash", hash),
	)
	return skipped, results, nil
}

func (v *VM) execute(lctx ApplyContext, ss *core.StagedCache, txs []types.Transaction) ([]types.TransactionWithResult, []types.Transaction, uint64, error) {
	var (
		rd          bytes.Reader
		decoder     = scale.NewDecoder(&rd)
		fees        uint64
		ineffective []types.Transaction
		executed    []types.TransactionWithResult
		limit       = v.cfg.GasLimit
	)
	for i := range txs {
		logger := v.logger.WithFields(log.Int("ith", i))
		txCount.Inc()

		t1 := time.Now()
		tx := txs[i]

		rd.Reset(tx.GetRaw().Raw)
		req := &Request{
			vm:      v,
			cache:   ss,
			lid:     lctx.Layer,
			raw:     txs[i].GetRaw(),
			decoder: decoder,
		}

		header, err := req.Parse()
		if err != nil {
			logger.With().Warning("ineffective transaction. failed to parse",
				tx.GetRaw().ID,
				log.Err(err),
			)
			ineffective = append(ineffective, types.Transaction{RawTx: tx.GetRaw()})
			invalidTxCount.Inc()
			continue
		}
		ctx := req.ctx
		args := req.args

		if header.GasPrice == 0 {
			logger.With().Warning("ineffective transaction. zero gas price",
				log.Object("header", header),
				log.Object("account", &ctx.PrincipalAccount),
			)
			ineffective = append(ineffective, types.Transaction{RawTx: tx.GetRaw()})
			invalidTxCount.Inc()
			continue
		}
		if intrinsic := core.IntrinsicGas(ctx.Gas.BaseGas, tx.GetRaw().Raw); ctx.PrincipalAccount.Balance < intrinsic {
			logger.With().Warning("ineffective transaction. intrinstic gas not covered",
				log.Object("header", header),
				log.Object("account", &ctx.PrincipalAccount),
				log.Uint64("intrinsic gas", intrinsic),
			)
			ineffective = append(ineffective, types.Transaction{RawTx: tx.GetRaw()})
			invalidTxCount.Inc()
			continue
		}
		if limit < ctx.Header.MaxGas {
			logger.With().Warning("ineffective transaction. out of block gas",
				log.Uint64("block gas limit", v.cfg.GasLimit),
				log.Uint64("current limit", limit),
				log.Object("header", header),
				log.Object("account", &ctx.PrincipalAccount),
			)
			ineffective = append(ineffective, types.Transaction{RawTx: tx.GetRaw()})
			invalidTxCount.Inc()
			continue
		}

		// NOTE this part is executed only for transactions that weren't verified
		// when saved into database by txs module
		if !tx.Verified() && !req.Verify() {
			logger.With().Warning("ineffective transaction. failed verify",
				log.Object("header", header),
				log.Object("account", &ctx.PrincipalAccount),
			)
			ineffective = append(ineffective, types.Transaction{RawTx: tx.GetRaw()})
			invalidTxCount.Inc()
			continue
		}

		if ctx.PrincipalAccount.NextNonce > ctx.Header.Nonce {
			logger.With().Warning("ineffective transaction. nonce too low",
				log.Object("header", header),
				log.Object("account", &ctx.PrincipalAccount),
			)
			ineffective = append(ineffective, types.Transaction{RawTx: tx.GetRaw(), TxHeader: header})
			invalidTxCount.Inc()
			continue
		}

		t2 := time.Now()
		logger.With().Debug("applying transaction",
			log.Object("header", header),
			log.Object("account", &ctx.PrincipalAccount),
		)

		rst := types.TransactionWithResult{}
		rst.Layer = lctx.Layer

		err = ctx.Consume(ctx.Header.MaxGas)
		if err == nil {
			err = ctx.PrincipalHandler.Exec(ctx, ctx.Header.Method, args)
		}
		if err != nil {
			logger.With().Debug("transaction failed",
				log.Object("header", header),
				log.Object("account", &ctx.PrincipalAccount),
				log.Err(err),
			)
			if errors.Is(err, core.ErrInternal) {
				return nil, nil, 0, err
			}
		}
		transactionDurationExecute.Observe(float64(time.Since(t2)))

		rst.RawTx = txs[i].GetRaw()
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
			return nil, nil, 0, fmt.Errorf("%w: %s", core.ErrInternal, err.Error())
		}
		fees += ctx.Fee()
		limit -= ctx.Consumed()

		executed = append(executed, rst)
		transactionDuration.Observe(float64(time.Since(t1)))
	}
	return executed, ineffective, fees, nil
}

// Request used to implement 2-step validation flow.
// After Parse is executed - conservative cache may do validation and skip Verify
// if transaction can't be executed.
type Request struct {
	vm    *VM
	cache *core.StagedCache

	lid     types.LayerID
	raw     types.RawTx
	decoder *scale.Decoder

	// both ctx and args are set after successful Parse
	ctx  *core.Context
	args scale.Encodable
}

// Parse header from the raw transaction.
func (r *Request) Parse() (*core.Header, error) {
	start := time.Now()
	if len(r.raw.Raw) > core.TxSizeLimit {
		return nil, fmt.Errorf("%w: tx size (%d) > limit (%d)", core.ErrTxLimit, len(r.raw.Raw), core.TxSizeLimit)
	}
	header, ctx, args, err := parse(r.vm.logger, r.lid, r.vm.registry, r.cache, r.vm.cfg, r.raw.Raw, r.decoder)
	if err != nil {
		return nil, err
	}
	r.ctx = ctx
	r.args = args
	transactionDurationParse.Observe(float64(time.Since(start)))
	return header, nil
}

// Verify transaction. Will panic if called without Parse completing successfully.
func (r *Request) Verify() bool {
	if r.ctx == nil {
		panic("Verify should be called after successful Parse")
	}
	start := time.Now()
	rst := verify(r.ctx, r.raw.Raw, r.decoder)
	transactionDurationVerify.Observe(float64(time.Since(start)))
	return rst
}

func parse(logger log.Log, lid types.LayerID, reg *registry.Registry, loader core.AccountLoader, cfg Config, raw []byte, decoder *scale.Decoder) (*core.Header, *core.Context, scale.Encodable, error) {
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

	ctx := &core.Context{
		GenesisID:        cfg.GenesisID,
		Registry:         reg,
		Loader:           loader,
		PrincipalAccount: account,
		LayerID:          lid,
	}

	if account.TemplateAddress != nil {
		ctx.PrincipalHandler = reg.Get(*account.TemplateAddress)
		if ctx.PrincipalHandler == nil {
			return nil, nil, nil, fmt.Errorf("%w: unknown template %s", core.ErrMalformed, *account.TemplateAddress)
		}
		ctx.PrincipalTemplate, err = ctx.PrincipalHandler.Load(account.State)
		if err != nil {
			return nil, nil, nil, err
		}
	}

	var (
		templateAddress *core.Address
		handler         core.Handler
	)
	if method == core.MethodSpawn {
		// the transaction is either spawn or self-spawn
		templateAddress = &core.Address{}
		if _, err := templateAddress.DecodeScale(decoder); err != nil {
			return nil, nil, nil, fmt.Errorf("%w failed to decode template address %s", core.ErrMalformed, err)
		}
		handler = reg.Get(*templateAddress)
		if handler == nil {
			return nil, nil, nil, fmt.Errorf("%w: unknown template %s", core.ErrMalformed, *templateAddress)
		}
		if ctx.PrincipalHandler == nil {
			// spawn is not possible if principal is not spawned
			// so this must be self-spawn, but we can't tell before decoding arguments
			ctx.PrincipalHandler = handler
		}
	} else {
		// this is any other call transaction
		if account.TemplateAddress == nil {
			return nil, nil, nil, core.ErrNotSpawned
		}
		templateAddress = account.TemplateAddress
		handler = ctx.PrincipalHandler
	}
	output, err := ctx.PrincipalHandler.Parse(ctx, method, decoder)
	if err != nil {
		return nil, nil, nil, err
	}
	args := handler.Args(method)
	if args == nil {
		return nil, nil, nil, fmt.Errorf("%w: unknown method %s %d", core.ErrMalformed, *templateAddress, method)
	}
	if _, err := args.DecodeScale(decoder); err != nil {
		return nil, nil, nil, fmt.Errorf("%w failed to decode method arguments %s", core.ErrMalformed, err)
	}
	if method == core.MethodSpawn {
		if core.ComputePrincipal(*templateAddress, args) == principal {
			// this is a self spawn. if it fails validation - discard it immediately
			ctx.PrincipalTemplate, err = ctx.PrincipalHandler.New(args)
			if err != nil {
				return nil, nil, nil, err
			}
			ctx.Gas.FixedGas += ctx.PrincipalTemplate.ExecGas(method)
		} else if account.TemplateAddress == nil {
			return nil, nil, nil, fmt.Errorf("%w: account can't spawn until it is spawned itself", core.ErrNotSpawned)
		} else {
			target, err := handler.New(args)
			if err != nil {
				return nil, nil, nil, err
			}
			ctx.Gas.FixedGas += ctx.PrincipalTemplate.LoadGas()
			ctx.Gas.FixedGas += target.ExecGas(method)
		}
	} else {
		ctx.Gas.FixedGas += ctx.PrincipalTemplate.LoadGas()
		ctx.Gas.FixedGas += ctx.PrincipalTemplate.ExecGas(method)
	}
	ctx.Gas.BaseGas = ctx.PrincipalTemplate.BaseGas(method)

	ctx.ParseOutput = output

	ctx.Header.Principal = principal
	ctx.Header.TemplateAddress = *templateAddress
	ctx.Header.Method = method
	ctx.Header.MaxGas = core.MaxGas(ctx.Gas.BaseGas, ctx.Gas.FixedGas, raw)
	ctx.Header.GasPrice = output.GasPrice
	ctx.Header.Nonce = output.Nonce
	ctx.Args = args

	maxspend, err := ctx.PrincipalTemplate.MaxSpend(ctx.Header.Method, args)
	if err != nil {
		return nil, nil, nil, err
	}
	ctx.Header.MaxSpend = maxspend
	return &ctx.Header, ctx, args, nil
}

func verify(ctx *core.Context, raw []byte, dec *scale.Decoder) bool {
	return ctx.PrincipalTemplate.Verify(ctx, raw, dec)
}

// ApplyContext has information on layer and block id.
type ApplyContext struct {
	Layer types.LayerID
}
