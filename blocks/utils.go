package blocks

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"math"
	"math/big"
	"math/rand"
	"sort"

	"github.com/seehuhn/mt19937"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/common/util"
	"github.com/spacemeshos/go-spacemesh/datastore"
	"github.com/spacemeshos/go-spacemesh/events"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/proposals"
	"github.com/spacemeshos/go-spacemesh/sql/layers"
	"github.com/spacemeshos/go-spacemesh/sql/transactions"
	"github.com/spacemeshos/go-spacemesh/txs"
)

var (
	errNodeHasBadMeshHash   = errors.New("node has different mesh hash from majority")
	errProposalTxMissing    = errors.New("proposal tx not found")
	errProposalTxHdrMissing = errors.New("proposal tx missing header")
)

type meshState struct {
	hash  types.Hash32
	count int
}

type proposalMetadata struct {
	lid        types.LayerID
	proposals  []*types.Proposal
	mtxs       []*types.MeshTransaction
	tickHeight uint64
	rewards    []types.AnyReward
	optFilter  bool
}

func getProposalMetadata(
	logger log.Log,
	cdb *datastore.CachedDB,
	cfg Config,
	lid types.LayerID,
	proposals []*types.Proposal,
) (*proposalMetadata, error) {
	var (
		md = &proposalMetadata{
			lid:       lid,
			proposals: proposals,
			mtxs:      []*types.MeshTransaction{},
		}
		seen       = make(map[types.TransactionID]struct{})
		meshHashes = make(map[types.Hash32]*meshState)
		err        error
	)
	md.tickHeight, md.rewards, err = extractCoinbasesAndHeight(logger, cdb, cfg, proposals)
	if err != nil {
		return nil, err
	}
	for _, p := range proposals {
		key := p.MeshHash
		if _, ok := meshHashes[key]; !ok {
			meshHashes[key] = &meshState{
				hash:  p.MeshHash,
				count: 1,
			}
		} else {
			meshHashes[key].count++
		}

		for _, tid := range p.TxIDs {
			if _, ok := seen[tid]; ok {
				continue
			}
			mtx, err := transactions.Get(cdb, tid)
			if err != nil {
				return nil, fmt.Errorf("%w: get proposal tx: %v", errProposalTxMissing, err.Error())
			}
			if mtx.TxHeader == nil {
				return nil, fmt.Errorf("%w: inconsistent state: tx %s is missing header", errProposalTxHdrMissing, mtx.ID)
			}
			seen[tid] = struct{}{}
			md.mtxs = append(md.mtxs, mtx)
		}
	}

	majority := cfg.OptFilterThreshold * len(proposals)
	var majorityState *meshState
	for _, ms := range meshHashes {
		logger.With().Debug("mesh hash",
			ms.hash,
			log.Int("count", ms.count),
			log.Int("threshold", cfg.OptFilterThreshold),
			log.Int("num_proposals", len(proposals)))
		if ms.hash != types.EmptyLayerHash && ms.count*100 >= majority {
			majorityState = ms
		}
	}
	if majorityState == nil {
		logger.With().Info("no consensus on mesh hash. NOT doing optimistic filtering", lid)
		return md, nil
	}
	ownMeshHash, err := layers.GetAggregatedHash(cdb, lid.Sub(1))
	if err != nil {
		return nil, fmt.Errorf("get prev mesh hash %w", err)
	}
	if ownMeshHash != majorityState.hash {
		return nil, fmt.Errorf("%w: majority %v, node %v", errNodeHasBadMeshHash, majorityState.hash, ownMeshHash)
	}
	md.optFilter = true
	logger.With().Info("consensus on mesh hash. doing optimistic filtering",
		lid,
		log.Stringer("mesh_hash", majorityState.hash))
	return md, nil
}

func getBlockTXs(logger log.Log, pmd *proposalMetadata, blockSeed []byte, gasLimit uint64) ([]types.TransactionID, error) {
	stateF := func(_ types.Address) (uint64, uint64) {
		return 0, math.MaxUint64
	}
	txCache := txs.NewCache(stateF, logger)
	if err := txCache.BuildFromTXs(pmd.mtxs, blockSeed); err != nil {
		return nil, fmt.Errorf("build txs for block: %w", err)
	}
	byAddrAndNonce := txCache.GetMempool(logger)
	if len(byAddrAndNonce) == 0 {
		logger.With().Warning("no feasible txs for block")
		return nil, nil
	}
	candidates := make([]*txs.NanoTX, 0, len(pmd.mtxs))
	byTid := make(map[types.TransactionID]*txs.NanoTX)
	for _, acctTXs := range byAddrAndNonce {
		candidates = append(candidates, acctTXs...)
		for _, ntx := range acctTXs {
			byTid[ntx.ID] = ntx
		}
	}
	sort.Slice(candidates, func(i, j int) bool { return candidates[i].ID.Compare(candidates[j].ID) })
	// initialize a Mersenne Twister with the block seed and use it as a source of randomness for
	// a Fisher-Yates shuffle of the sorted transaction IDs.
	mt := mt19937.New()
	mt.SeedFromSlice(toUint64Slice(blockSeed))
	rng := rand.New(mt)
	ordered := txs.ShuffleWithNonceOrder(logger, rng, len(candidates), candidates, byAddrAndNonce)
	if gasLimit > 0 {
		ordered = prune(logger, ordered, byTid, gasLimit)
	}
	return ordered, nil
}

func prune(logger log.Log, tids []types.TransactionID, byTid map[types.TransactionID]*txs.NanoTX, gasLimit uint64) []types.TransactionID {
	var (
		gasRemaining = gasLimit
		idx          int
		tid          types.TransactionID
	)
	for idx, tid = range tids {
		if gasRemaining < txs.MinTXGas {
			logger.With().Info("gas exhausted for block",
				log.Int("num_txs", idx),
				log.Uint64("gas_left", gasRemaining),
				log.Uint64("gas_limit", gasLimit))
			return tids[:idx]
		}
		ntx, ok := byTid[tid]
		if !ok {
			logger.With().Fatal("tx missing", tid)
		}
		gasRemaining -= ntx.MaxGas
	}
	logger.With().Debug("block txs after pruning", log.Int("num_txs", len(tids)))
	return tids
}

func toUint64Slice(b []byte) []uint64 {
	const numByte = 8
	l := len(b)
	var s []uint64
	for i := 0; i < l; i += numByte {
		s = append(s, binary.LittleEndian.Uint64(b[i:util.Min(l, i+numByte)]))
	}
	return s
}

func extractCoinbasesAndHeight(logger log.Log, cdb *datastore.CachedDB, cfg Config, props []*types.Proposal) (uint64, []types.AnyReward, error) {
	weights := make(map[types.Address]*big.Rat)
	coinbases := make([]types.Address, 0, len(props))
	max := uint64(0)
	for _, p := range props {
		if p.AtxID == *types.EmptyATXID {
			// this proposal would not have been validated
			logger.With().Error("proposal with invalid ATXID, skipping reward distribution", p.Layer, p.ID())
			return 0, nil, errInvalidATXID
		}
		atx, err := cdb.GetAtxHeader(p.AtxID)
		if atx.BaseTickHeight > max {
			max = atx.BaseTickHeight
		}
		if err != nil {
			logger.With().Warning("proposal ATX not found", p.ID(), p.AtxID, log.Err(err))
			return 0, nil, fmt.Errorf("block gen get ATX: %w", err)
		}
		ballot := &p.Ballot
		weightPer, err := proposals.ComputeWeightPerEligibility(cdb, ballot, cfg.LayerSize, cfg.LayersPerEpoch)
		if err != nil {
			logger.With().Error("failed to calculate weight per eligibility", p.ID(), log.Err(err))
			return 0, nil, err
		}
		logger.With().Debug("weight per eligibility", p.ID(), log.Stringer("weight_per", weightPer))
		actual := weightPer.Mul(weightPer, new(big.Rat).SetUint64(uint64(len(ballot.EligibilityProofs))))
		if weight, ok := weights[atx.Coinbase]; !ok {
			weights[atx.Coinbase] = actual
			coinbases = append(coinbases, atx.Coinbase)
		} else {
			weights[atx.Coinbase].Add(weight, actual)
		}
		events.ReportProposal(events.ProposalIncluded, p)
	}
	// make sure we output coinbase in a stable order.
	sort.Slice(coinbases, func(i, j int) bool {
		return bytes.Compare(coinbases[i].Bytes(), coinbases[j].Bytes()) < 0
	})
	rewards := make([]types.AnyReward, 0, len(weights))
	for _, coinbase := range coinbases {
		weight, ok := weights[coinbase]
		if !ok {
			logger.With().Fatal("coinbase missing", coinbase)
		}
		rewards = append(rewards, types.AnyReward{
			Coinbase: coinbase,
			Weight: types.RatNum{
				Num:   weight.Num().Uint64(),
				Denom: weight.Denom().Uint64(),
			},
		})
		logger.With().Debug("adding coinbase weight",
			log.Stringer("coinbase", coinbase),
			log.Stringer("weight", weight))
	}
	return max, rewards, nil
}
