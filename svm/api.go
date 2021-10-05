package svm

import (
	"fmt"
	"math/big"

	"github.com/spacemeshos/go-spacemesh/api/config"
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/common/util"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/mesh"
	"github.com/spacemeshos/go-spacemesh/svm/state"
)

// SVM is an entry point for all SVM operations.
type SVM struct {
	state *state.TransactionProcessor
	log   log.Logger
}

func (s *SVM) TxProcessor() mesh.TxProcessor {
	return s.state
}

// New creates a new `SVM` instance from the given `state` and `logger`.
func New(state *state.TransactionProcessor, logger log.Log) *SVM {
	return &SVM{state, log.NewDefault("svm")}
}

// SetupGenesis creates new accounts and adds balances as dictated by `conf`.
func (svm *SVM) SetupGenesis(conf *config.GenesisConfig) error {
	if conf == nil {
		conf = config.DefaultGenesisConfig()
	}
	for id, balance := range conf.Accounts {
		bytes := util.FromHex(id)
		if len(bytes) == 0 {
			return fmt.Errorf("cannot decode entry %s for genesis account", id)
		}
		// just make it explicit that we want address and not a public key
		if len(bytes) != types.AddressLength {
			return fmt.Errorf("%s must be an address of size %d", id, types.AddressLength)
		}
		addr := types.BytesToAddress(bytes)
		svm.state.CreateAccount(addr)
		svm.AddBalance(addr, balance)
		svm.log.With().Info("genesis account created",
			log.String("address", addr.Hex()),
			log.Uint64("balance", balance))
	}

	_, err := svm.state.Commit()
	if err != nil {
		return fmt.Errorf("cannot commit genesis state: %w", err)
	}
	return nil
}

func (s *SVM) AddBalance(addr types.Address, amount uint64) {
	s.state.AddBalance(addr, amount)
}

func txsGetPointers(transactions []types.Transaction) []*types.Transaction {
	txs := make([]*types.Transaction, 0)
	for _, t := range transactions {
		txs = append(txs, &t)
	}

	return txs
}

// ApplyLayer should be called when a layer is applied, i.e. after Hare/Tortoise
// verification.
func (s *SVM) ApplyLayer(layerID types.LayerID, transactions []*types.Transaction, rewards []types.AmountAndAddress) (failed []*types.Transaction, err error) {
	for _, reward := range rewards {
		s.AddBalance(reward.Address, reward.Amount)
	}

	remainingTxs, err := s.state.ApplyTransactions(layerID, transactions)
	if err != nil {
		return remainingTxs, err
	}

	return remainingTxs, nil
}

func (s *SVM) writeTransactionRewards(l types.LayerID, accountBlockCount map[types.Address]map[string]uint64, totalReward, layerReward *big.Int) error {
	for account, smesherAccountEntry := range accountBlockCount {
		for _, cnt := range smesherAccountEntry {
			reward := cnt * totalReward.Uint64()
			s.AddBalance(account, reward)
		}
	}

	return nil
}
