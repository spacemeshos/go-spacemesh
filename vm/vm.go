package vm

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"time"

	gossamerScale "github.com/ChainSafe/gossamer/pkg/scale"
	athcon "github.com/athenavm/athena/ffi/athcon/bindings/go"
	"github.com/spacemeshos/go-scale"
	"go.uber.org/zap"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/events"
	"github.com/spacemeshos/go-spacemesh/hash"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/sql"
	"github.com/spacemeshos/go-spacemesh/sql/accounts"
	"github.com/spacemeshos/go-spacemesh/sql/layers"
	"github.com/spacemeshos/go-spacemesh/sql/rewards"
	"github.com/spacemeshos/go-spacemesh/sql/transactions"
	"github.com/spacemeshos/go-spacemesh/system"
	"github.com/spacemeshos/go-spacemesh/vm/core"
	vmhost "github.com/spacemeshos/go-spacemesh/vm/host"
	// FIXME: move Wallet methods New, MaxSpend and Verify out of 'templates/wallet'
	// as they are generic.
	"github.com/spacemeshos/go-spacemesh/vm/templates/wallet"
)

// Opt is for changing VM during initialization.
type Opt func(*VM)

// WithLogger sets logger for VM.
func WithLogger(logger *zap.Logger) Opt {
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
func New(db sql.StateDatabase, opts ...Opt) *VM {
	vm := &VM{
		logger: zap.NewNop(),
		db:     db,
		cfg:    DefaultConfig(),
	}
	for _, opt := range opts {
		opt(vm)
	}
	return vm
}

// VM handles modifications to the account state.
type VM struct {
	logger *zap.Logger
	db     sql.StateDatabase
	cfg    Config
}

// Validation initializes validation request.
func (v *VM) Validation(raw types.RawTx) system.ValidationRequest {
	return &Request{
		vm:    v,
		cache: core.NewStagedCache(core.DBLoader{Executor: v.db}),
		raw:   raw,
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
	v.logger.Info("vm reverted to layer", zap.Uint32("layer", lid.Uint32()))
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
		v.logger.Info("genesis account", zap.Inline(account))
		if err := accounts.Update(tx, account); err != nil {
			return fmt.Errorf("inserting genesis account: %w", err)
		}
	}

	return tx.Commit()
}

// Apply transactions.
func (v *VM) Apply(
	layer types.LayerID,
	txs []types.Transaction,
	blockRewards []types.CoinbaseReward,
) ([]types.Transaction, []types.TransactionWithResult, error) {
	if layer.Before(types.GetEffectiveGenesis()) {
		return nil, nil, fmt.Errorf("%w: applying layer %s before effective genesis %s",
			core.ErrInternal, layer, types.GetEffectiveGenesis(),
		)
	}
	t1 := time.Now()

	ss := core.NewStagedCache(core.DBLoader{Executor: v.db})
	results, skipped, fees, err := v.execute(layer, ss, txs)
	if err != nil {
		return nil, nil, err
	}
	t2 := time.Now()
	blockDurationTxs.Observe(float64(time.Since(t1)))

	rewardsResult, err := v.addRewards(layer, ss, fees, blockRewards)
	if err != nil {
		return nil, nil, err
	}

	t3 := time.Now()
	blockDurationRewards.Observe(float64(time.Since(t2)))

	hasher := hash.GetHasher()
	encoder := scale.NewEncoder(hasher)
	total := 0

	tx, err := v.db.TxImmediate(context.Background())
	if err != nil {
		return nil, nil, err
	}
	defer tx.Release()
	t4 := time.Now()
	blockDurationWait.Observe(float64(time.Since(t3)))

	for _, reward := range rewardsResult {
		if err := rewards.Add(tx, &reward); err != nil {
			return nil, nil, fmt.Errorf("%w: %w", core.ErrInternal, err)
		}
	}

	ss.IterateChanged(func(account *core.Account) bool {
		total++
		account.Layer = layer
		v.logger.Debug("update account state", zap.Inline(account))
		if err = accounts.Update(tx, account); err != nil {
			return false
		}
		account.EncodeScale(encoder)
		return true
	})
	if err != nil {
		return nil, nil, fmt.Errorf("%w: %w", core.ErrInternal, err)
	}
	writesPerBlock.Observe(float64(total))

	var hashSum types.Hash32
	hasher.Sum(hashSum[:0])
	if err := layers.UpdateStateHash(tx, layer, hashSum); err != nil {
		return nil, nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, nil, fmt.Errorf("%w: %w", core.ErrInternal, err)
	}
	ss.IterateChanged(func(account *core.Account) bool {
		if err := events.ReportAccountUpdate(account.Address); err != nil {
			v.logger.Error("Failed to emit account update",
				zap.Stringer("account", account.Address),
				zap.Error(err),
			)
		}
		return true
	})
	for _, reward := range rewardsResult {
		if err := events.ReportRewardReceived(reward); err != nil {
			v.logger.Error("Failed to emit rewards", zap.Uint32("lid", reward.Layer.Uint32()), zap.Error(err))
		}
	}
	hash.PutHasher(hasher)

	blockDurationPersist.Observe(float64(time.Since(t4)))
	blockDuration.Observe(float64(time.Since(t1)))
	transactionsPerBlock.Observe(float64(len(txs)))
	appliedLayer.Set(float64(layer))

	v.logger.Debug("applied layer",
		zap.Uint32("layer", layer.Uint32()),
		zap.Int("count", len(txs)-len(skipped)),
		zap.Duration("duration", time.Since(t1)),
		zap.Stringer("state_hash", hashSum),
	)
	return skipped, results, nil
}

func (v *VM) execute(
	layer types.LayerID,
	ss *core.StagedCache,
	txs []types.Transaction,
) ([]types.TransactionWithResult, []types.Transaction, uint64, error) {
	var (
		fees        uint64
		ineffective []types.Transaction
		executed    []types.TransactionWithResult
		limit       = v.cfg.GasLimit
	)
	for i, tx := range txs {
		logger := v.logger.With(zap.Int("txnum", i))
		txCount.Inc()

		t1 := time.Now()

		req := &Request{
			vm:    v,
			cache: ss,
			lid:   layer,
			raw:   tx.GetRaw(),
		}

		header, err := req.Parse()
		if err != nil {
			logger.Warn("ineffective transaction. failed to parse",
				log.ZShortStringer("tx", tx.GetRaw().ID),
				zap.Error(err),
			)
			ineffective = append(ineffective, types.Transaction{RawTx: tx.GetRaw()})
			invalidTxCount.Inc()
			continue
		}
		ctx := req.ctx

		if header.GasPrice == 0 {
			logger.Warn("ineffective transaction. zero gas price",
				zap.Object("header", header),
				zap.Stringer("account", ctx.Principal()),
			)
			ineffective = append(ineffective, types.Transaction{RawTx: tx.GetRaw()})
			invalidTxCount.Inc()
			continue
		}
		balance := ctx.Balance()
		intrinsic := core.IntrinsicGas(ctx.PrincipalTemplate.BaseGas(), len(tx.GetRaw().Raw))
		logger.Info("intrinsic gas check", zap.Uint64("balance", balance), zap.Uint64("intrinsic gas", intrinsic))
		if balance < intrinsic {
			logger.Warn("ineffective transaction. intrinsic gas not covered",
				zap.Object("header", header),
				zap.Stringer("account", ctx.Principal()),
				zap.Uint64("intrinsic gas", intrinsic),
			)
			ineffective = append(ineffective, types.Transaction{RawTx: tx.GetRaw()})
			invalidTxCount.Inc()
			continue
		}
		if limit < ctx.Header.MaxGas {
			logger.Warn("ineffective transaction. out of block gas",
				zap.Uint64("max gas", ctx.Header.MaxGas),
				zap.Uint64("block gas limit", v.cfg.GasLimit),
				zap.Uint64("current limit", limit),
				zap.Object("header", header),
				zap.Stringer("account", ctx.Principal()),
			)
			ineffective = append(ineffective, types.Transaction{RawTx: tx.GetRaw()})
			invalidTxCount.Inc()
			continue
		}

		// NOTE this part is executed only for transactions that weren't verified
		// when saved into database by txs module
		if !tx.Verified() {
			if err := req.Verify(); err != nil {
				logger.Warn("ineffective transaction. failed verify",
					zap.Stringer("txid", tx.GetRaw().ID),
					zap.Object("header", header),
					zap.Stringer("account", ctx.Principal()),
					zap.Int("payload size", len(ctx.Payload())),
					zap.Error(err),
				)
				ineffective = append(ineffective, types.Transaction{RawTx: tx.GetRaw()})
				invalidTxCount.Inc()
				continue
			}
		}

		if ctx.NextNonce() > ctx.Header.Nonce {
			logger.Warn("ineffective transaction. nonce too low",
				zap.Object("header", header),
				zap.Stringer("account", ctx.Principal()),
			)
			ineffective = append(ineffective, types.Transaction{RawTx: tx.GetRaw(), TxHeader: header})
			invalidTxCount.Inc()
			continue
		}

		t2 := time.Now()
		logger.Debug("applying transaction",
			zap.Stringer("txid", tx.GetRaw().ID),
			zap.Object("header", header),
			zap.Stringer("account", ctx.Principal()),
			zap.Int("payload size", len(ctx.Payload())),
		)

		rst := types.TransactionWithResult{}
		rst.Layer = layer

		err = ctx.Consume(ctx.Header.MaxGas)
		if err == nil {
			err = v.execInVm(ctx, ctx.Payload())
		}
		if err == nil {
			// If tx succeeded, refund remaining gas
			// (We consume all remaining gas if the tx failed)
			rst.Status = types.TransactionSuccess
			ctx.Refund()
		} else {
			logger.Debug("transaction failed, skipping gas refund",
				zap.Object("header", header),
				zap.Stringer("account", ctx.Principal()),
				zap.Error(err),
			)
			if errors.Is(err, core.ErrInternal) {
				return nil, nil, 0, err
			}
			rst.Status = types.TransactionFailure
			rst.Message = err.Error()
		}
		transactionDurationExecute.Observe(float64(time.Since(t2)))

		rst.RawTx = txs[i].GetRaw()
		rst.TxHeader = &ctx.Header
		rst.Gas = ctx.Consumed()
		rst.Fee = ctx.Fee()
		rst.Addresses = ctx.Updated()

		if err := ctx.Apply(ss); err != nil {
			return nil, nil, 0, fmt.Errorf("%w: %w", core.ErrInternal, err)
		}
		fees += ctx.Fee()
		limit -= ctx.Consumed()

		executed = append(executed, rst)
		transactionDuration.Observe(float64(time.Since(t1)))
	}
	return executed, ineffective, fees, nil
}

func (v *VM) execInVm(host *core.Context, payload []byte) error {
	templateAccount, err := host.Get(host.Header.TemplateAddress)
	if err != nil {
		return fmt.Errorf("failed to load template account: %w", err)
	} else if len(templateAccount.State) == 0 {
		return errors.New("template account state is empty")
	}

	vmhost, err := vmhost.NewHost(host, v.logger)
	if err != nil {
		return fmt.Errorf("failed to instantiate VM: %w", err)
	}
	defer vmhost.Destroy()

	// sanity check - verify should have failed for this tx
	if host.IsSpawn() && len(host.PrincipalAccount.State) > 0 {
		return errors.New("wallet account state is not empty for spawn")
	}
	executionPayload := athcon.EncodedExecutionPayload(host.PrincipalAccount.State, payload)

	// Execute the transaction in the VM
	// Note: at this point, maxgas was already consumed from the principal account, so we don't
	// need to check the account balance, but we still need to communicate the amount to the VM
	// so it can short-circuit execution if the amount is exceeded.
	maxgas := int64(host.MaxGas() - host.GasSpent())
	if maxgas < 0 {
		return errors.New("gas limit exceeds maximum int64 value")
	}
	v.logger.Debug("executing", zap.Uint32("layer", host.LayerID.Uint32()), zap.Int64("maxgas", maxgas))
	_, gasLeft, err := vmhost.Execute(
		host.Layer(),
		maxgas,
		host.Principal(),
		host.Principal(),
		executionPayload,
		templateAccount.State,
	)
	host.SpendGas(uint64(maxgas) - uint64(gasLeft))
	return err
}

// Request used to implement 2-step validation flow.
// After Parse is executed - conservative cache may do validation and skip Verify
// if transaction can't be executed.
type Request struct {
	vm    *VM
	cache *core.StagedCache

	lid types.LayerID
	raw types.RawTx

	// ctx set after successful Parse
	ctx *core.Context
}

// Parse header from the raw transaction.
func (r *Request) Parse() (*core.Header, error) {
	start := time.Now()
	if len(r.raw.Raw) > core.TxSizeLimit {
		return nil, fmt.Errorf("%w: tx size (%d) > limit (%d)", core.ErrTxLimit, len(r.raw.Raw), core.TxSizeLimit)
	}
	header, ctx, err := parse(r.vm.logger, r.lid, r.cache, r.vm.cfg, r.raw.Raw)
	if err != nil {
		return nil, err
	}
	r.ctx = ctx
	transactionDurationParse.Observe(float64(time.Since(start)))
	return header, nil
}

// Verify transaction. Will panic if called before Parse completes succcessfully.
func (r *Request) Verify() error {
	if r.ctx == nil {
		panic("Verify should be called after successful Parse")
	}
	start := time.Now()
	rst := verify(r.ctx)
	transactionDurationVerify.Observe(float64(time.Since(start)))
	return rst
}

var (
	errWrongVersion    = errors.New("wrong version")
	errUnknownTemplate = errors.New("unknown template")
)

func parse(
	logger *zap.Logger,
	lid types.LayerID,
	loader core.AccountLoader,
	cfg Config,
	raw []byte,
) (*core.Header, *core.Context, error) {
	var tx core.Tx
	decoder := scale.NewDecoder(bytes.NewReader(raw))
	n, err := tx.DecodeScale(decoder)
	if err != nil {
		return nil, nil, fmt.Errorf("%w: decoding TX: %w", core.ErrMalformed, err)
	}
	// v1 is athena compatible tx
	if tx.Version != 1 {
		return nil, nil, fmt.Errorf("%w: %d", errWrongVersion, tx.Version)
	}
	// Decode witness data
	witnessData, _, err := scale.DecodeByteSlice(decoder)
	switch {
	case errors.Is(err, io.EOF):
		logger.Debug("witness data is empty")
	case err != nil:
		return nil, nil, fmt.Errorf("decoding witness data: %w", err)
	}

	ctx, err := core.New(cfg.GenesisID, lid, tx.Principal, loader, logger)
	if err != nil {
		return nil, nil, fmt.Errorf("creating new context: %w", err)
	}

	// There are two cases to consider:
	// 1. Principal account exists, and is spawned. In this case, we use the principal account
	// template.
	// 2. Principal account exists as a stub, but has not been spawned. Account is not spawned if it
	// doesn't have a template addressassigned. In this case, we assume the tx is a self-spawn for
	// the principal, and check that the calculated principal matches.

	// NOTE: Athena currently does not allow a tx with principal A to directly call a method on
	// template B where A != B. That will be handled by "proxied calls", where the target template
	// is passed not explicitly as part of the tx, but implicitly in the args. This simplifies the
	// logic here considerably.

	if ctx.PrincipalAccount.TemplateAddress == nil {
		if tx.Template == nil {
			return nil, nil, core.ErrNotSpawned
		}
		ctx.SpawnTx = true
		ctx.Header.TemplateAddress = *tx.Template
		// in case of a self-spawn, we need to check that the calculated principal matches.
		// only check this in case of spawn, because otherwise the payload may be for spend not spawn.
		// NOTE: this check isn't strictly necessary. this tx will fail verify later, since the
		// account will be spawned to the wrong location, but it's much cheaper to perform this check
		// now and fail fast.
		var payload athcon.Payload
		err = gossamerScale.Unmarshal(tx.Payload, &payload)
		if err != nil {
			return nil, nil, fmt.Errorf("%w: decoding TX payload: %w", core.ErrMalformed, err)
		}
		computedPrincipal := core.ComputePrincipalFromBlob(*tx.Template, payload.Input)
		if computedPrincipal != tx.Principal {
			return nil, nil, fmt.Errorf("computed spawn principal %q doesn't match %q", computedPrincipal, tx.Principal)
		}
	} else {
		if tx.Template != nil {
			return nil, nil, fmt.Errorf("%w: principal account already spawned", core.ErrMalformed)
		}
		ctx.Header.TemplateAddress = *ctx.PrincipalAccount.TemplateAddress
	}

	has, err := loader.Has(ctx.Header.TemplateAddress)
	if err != nil {
		return nil, nil, fmt.Errorf("%w: checking if template exists: %w", core.ErrInternal, err)
	}
	if !has {
		return nil, nil, fmt.Errorf("%w: %s", errUnknownTemplate, ctx.Header.TemplateAddress)
	}
	ctx.TxPayload = tx.Payload
	ctx.TxData = raw[:n]
	ctx.WitnessData = witnessData

	// At this point we've established that the transaction is correctly formed, but we haven't
	// yet attempted to validate the signature. That happens later in Verify().
	// FIXME: move New, Verify and MaxSpend methods out of `templates/wallet` package.
	ctx.PrincipalTemplate, err = wallet.New(ctx, logger.Named("template"))
	if err != nil {
		return nil, nil, fmt.Errorf("%w: creating principal handler: %w", core.ErrInternal, err)
	}

	// FIXME: How to obtain a max gas? Should it be returned from Verify()?
	ctx.Header.MaxGas = core.MaxGas(max(len(tx.Payload), 6) - 6) // skip bytes for method selector
	ctx.Header.Principal = tx.Principal
	ctx.Header.GasPrice = tx.Metadata.GasPrice
	ctx.Header.Nonce = tx.Metadata.Nonce

	maxspend, err := ctx.PrincipalTemplate.MaxSpend(tx.Payload)
	if err != nil {
		return nil, nil, err
	}
	ctx.Header.MaxSpend = maxspend
	return &ctx.Header, ctx, nil
}

func verify(ctx *core.Context) error {
	return ctx.PrincipalTemplate.Verify(ctx.TxData, ctx.WitnessData)
}
