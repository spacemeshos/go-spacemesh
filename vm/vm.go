package vm

import (
	"errors"
	"fmt"
	"math"

	"github.com/spacemeshos/go-spacemesh/api/config"
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/common/util"
	"github.com/spacemeshos/go-spacemesh/events"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/sql"
	"github.com/spacemeshos/go-spacemesh/sql/accounts"
	"github.com/spacemeshos/go-spacemesh/sql/layers"
	"github.com/spacemeshos/go-spacemesh/sql/transactions"
)

// RewardConfig defines the configuration options for Spacemesh rewards.
type RewardConfig struct {
	BaseReward uint64 `mapstructure:"base-reward"`
}

// DefaultRewardConfig returns the default RewardConfig.
func DefaultRewardConfig() RewardConfig {
	return RewardConfig{
		BaseReward: 50 * uint64(math.Pow10(12)),
	}
}

func calculateLayerReward(cfg RewardConfig) uint64 {
	// todo: add inflation rules here
	return cfg.BaseReward
}

// VM manages accounts state.
type VM struct {
	log log.Log
	db  *sql.Database
	cfg RewardConfig
}

// New creates a new `vm` instance from the given `state` and `logger`.
func New(logger log.Log, db *sql.Database) *VM {
	return &VM{
		db:  db,
		log: logger,
		cfg: DefaultRewardConfig(),
	}
}

// SetupGenesis creates new accounts and adds balances as dictated by `conf`.
func (vm *VM) SetupGenesis(genesis *config.GenesisConfig) error {
	if genesis == nil {
		genesis = config.DefaultGenesisConfig()
	}
	ss := newChanges(vm.log, vm.db, types.GetEffectiveGenesis())
	for id, balance := range genesis.Accounts {
		bytes := util.FromHex(id)
		if len(bytes) == 0 {
			return fmt.Errorf("cannot decode entry %s for genesis account", id)
		}
		// just make it explicit that we want address and not a public key
		if len(bytes) != types.AddressLength {
			return fmt.Errorf("%s must be an address of size %d", id, types.AddressLength)
		}
		addr := types.BytesToAddress(bytes)
		ss.addBalance(addr, balance)
		vm.log.With().Info("genesis account created",
			log.Stringer("address", addr),
			log.Uint64("balance", balance))
	}

	if _, err := ss.commit(); err != nil {
		return fmt.Errorf("cannot commit genesis state: %w", err)
	}
	return nil
}

// ApplyLayer applies the given rewards to some miners as well as a vector of
// transactions for the given layer. to miners vector for layer. It returns an
// error on failure, as well as a vector of failed transactions.
func (vm *VM) ApplyLayer(lid types.LayerID, txs []*types.Transaction, rewards []types.AnyReward) ([]*types.Transaction, []*types.Reward, error) {
	vm.log.With().Info("apply layer to vm",
		lid,
		log.Int("rewards", len(rewards)),
		log.Int("transactions", len(txs)),
	)
	ch := newChanges(vm.log, vm.db, lid)

	var (
		failed    []*types.Transaction
		totalFees uint64
	)
	for {
		for _, tx := range txs {
			if err := applyTransaction(vm.log, ch, tx); err != nil {
				if errors.Is(err, errInvalid) {
					failed = append(failed, tx)
				} else {
					return nil, nil, err
				}
			} else {
				totalFees += tx.GetFee()
			}
			events.ReportNewTx(lid, tx)
			events.ReportAccountUpdate(tx.Origin())
			events.ReportAccountUpdate(tx.GetRecipient())
		}
		if len(failed) == len(txs) {
			break
		}
		txs = failed
		failed = nil
	}

	finalRewards, err := calculateRewards(vm.log, vm.cfg, lid, totalFees, rewards)
	if err != nil {
		return nil, nil, err
	}
	for _, reward := range finalRewards {
		if err = ch.addBalance(reward.Coinbase, reward.TotalReward); err != nil {
			return nil, nil, err
		}
		vm.log.With().Info("coinbase rewarded",
			log.Stringer("coinbase", reward.Coinbase),
			log.Uint64("reward", reward.TotalReward))
		events.ReportAccountUpdate(reward.Coinbase)
	}
	if _, err = ch.commit(); err != nil {
		return nil, nil, err
	}
	return failed, finalRewards, nil
}

func calculateRewards(logger log.Log, cfg RewardConfig, lid types.LayerID, totalFees uint64, rewards []types.AnyReward) ([]*types.Reward, error) {
	logger = logger.WithFields(lid)
	totalWeight := util.WeightFromUint64(0)
	byCoinbase := make(map[types.Address]util.Weight)
	for _, reward := range rewards {
		weight := util.WeightFromUint64(0)
		if err := weight.GobDecode(reward.Weight); err != nil {
			logger.With().Error("failed to decode weight", log.Stringer("coinbase", reward.Coinbase))
			return nil, err
		}
		logger.With().Debug("coinbase weight", reward.Coinbase, log.Stringer("weight", weight))
		if weight.Sign() == -1 {
			logger.With().Error("invalid weight value",
				log.Stringer("coinbase", reward.Coinbase),
				log.Stringer("weight", weight))
			return nil, fmt.Errorf("invalid weight value %v: %v", reward.Coinbase.String(), reward.Weight)
		}
		totalWeight.Add(weight)
		if _, ok := byCoinbase[reward.Coinbase]; ok {
			byCoinbase[reward.Coinbase].Add(weight)
		} else {
			byCoinbase[reward.Coinbase] = weight
		}
	}
	if totalWeight.Cmp(util.WeightFromUint64(0)) == 0 {
		logger.Error("zero total weight in block rewards")
		return nil, fmt.Errorf("zero total weight")
	}
	finalRewards := make([]*types.Reward, 0, len(rewards))
	layerRewards := calculateLayerReward(cfg)
	totalRewards := layerRewards + totalFees
	logger.With().Info("rewards info for layer",
		log.Uint64("layer_rewards", layerRewards),
		log.Uint64("fee", totalFees))
	rewardPer := util.WeightFromUint64(totalRewards).Div(totalWeight)
	lyrRewardPer := util.WeightFromUint64(layerRewards).Div(totalWeight)
	seen := make(map[types.Address]struct{})
	for _, reward := range rewards {
		if _, ok := seen[reward.Coinbase]; ok {
			continue
		}

		seen[reward.Coinbase] = struct{}{}
		weight, ok := byCoinbase[reward.Coinbase]
		if !ok {
			logger.With().Fatal("missing weight for coinbase", log.Stringer("coinbase", reward.Coinbase))
		}
		fTotal, _ := rewardPer.Copy().Mul(weight).Float64()
		totalReward := uint64(fTotal)
		fLyr, _ := lyrRewardPer.Copy().Mul(weight).Float64()
		finalRewards = append(finalRewards, &types.Reward{
			Layer:       lid,
			Coinbase:    reward.Coinbase,
			TotalReward: totalReward,
			LayerReward: uint64(fLyr),
		})
		events.ReportAccountUpdate(reward.Coinbase)
	}
	return finalRewards, nil
}

// GetLayerStateRoot returns the state root at a given layer.
func (vm *VM) GetLayerStateRoot(lid types.LayerID) (types.Hash32, error) {
	return layers.GetStateHash(vm.db, lid)
}

// GetLayerApplied returns layer of the applied transaction.
func (vm *VM) GetLayerApplied(tid types.TransactionID) (types.LayerID, error) {
	return transactions.GetAppliedLayer(vm.db, tid)
}

// GetStateRoot gets the current state root hash.
func (vm *VM) GetStateRoot() (types.Hash32, error) {
	return layers.GetLatestStateHash(vm.db)
}

// Revert all changes that we made after the layer. Returns state hash of the layer.
func (vm *VM) Revert(lid types.LayerID) (types.Hash32, error) {
	err := accounts.Revert(vm.db, lid)
	if err != nil {
		return types.Hash32{}, err
	}
	return vm.GetStateRoot()
}

func (vm *VM) account(address types.Address) (types.Account, error) {
	return accounts.Latest(vm.db, address)
}

// AddressExists checks if an account address exists in this node's global state.
func (vm *VM) AddressExists(addr types.Address) (bool, error) {
	acc, err := vm.account(addr)
	if err != nil {
		return false, err
	}
	return acc.Layer.Value > 0, nil
}

// GetBalance Retrieve the balance from the given address or 0 if object not found.
func (vm *VM) GetBalance(addr types.Address) (uint64, error) {
	acc, err := vm.account(addr)
	if err != nil {
		return 0, err
	}
	return acc.Balance, nil
}

// GetNonce gets the current nonce of the given addr, if the address is not
// found it returns 0.
func (vm *VM) GetNonce(addr types.Address) (uint64, error) {
	acc, err := vm.account(addr)
	if err != nil {
		return 0, err
	}
	if !acc.Initialized {
		return 0, nil
	}
	return acc.Nonce + 1, nil
}

// GetAllAccounts returns a dump of all accounts in global state.
func (vm *VM) GetAllAccounts() ([]*types.Account, error) {
	return accounts.All(vm.db)
}

var (
	errInvalid = errors.New("invalid tx")
	errFunds   = fmt.Errorf("%w: insufficient funds", errInvalid)
	errNonce   = fmt.Errorf("%w: incorrect nonce", errInvalid)
)

// applyTransaction applies provided transaction to the current state, but does not commit it to persistent
// storage. It returns an error if the transaction is invalid, i.e., if there is not enough balance in the source
// account to perform the transaction and pay the fee or if the nonce is incorrect.
func applyTransaction(logger log.Log, ch *changes, tx *types.Transaction) error {
	balance, err := ch.balance(tx.Origin())
	if err != nil {
		return err
	}
	if total := tx.GetFee() + tx.Amount; balance < total {
		logger.With().Warning("not enough funds",
			log.Uint64("balance_have", balance),
			log.Uint64("balance_need", total))
		return errFunds
	}
	nonce, err := ch.nextNonce(tx.Origin())
	if err != nil {
		return err
	}
	if nonce != tx.AccountNonce {
		logger.With().Warning("invalid nonce",
			log.Uint64("nonce_correct", nonce),
			log.Uint64("nonce_actual", tx.AccountNonce))
		return errNonce
	}
	if err := ch.setNonce(tx.Origin(), tx.AccountNonce); err != nil {
		return err
	}
	if err := ch.addBalance(tx.GetRecipient(), tx.Amount); err != nil {
		return err
	}
	if err := ch.subBalance(tx.Origin(), tx.Amount); err != nil {
		return err
	}
	if err := ch.subBalance(tx.Origin(), tx.GetFee()); err != nil {
		return err
	}
	logger.With().Info("transaction processed", log.Stringer("transaction", tx))
	return nil
}
