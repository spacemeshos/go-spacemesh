package blocks

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"math"
	"math/big"
	"math/rand/v2"
	"sort"

	"github.com/seehuhn/mt19937"
	"golang.org/x/exp/maps"

	"github.com/spacemeshos/go-spacemesh/atxsdata"
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/events"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/sql"
	"github.com/spacemeshos/go-spacemesh/sql/ballots"
	"github.com/spacemeshos/go-spacemesh/sql/layers"
	"github.com/spacemeshos/go-spacemesh/sql/transactions"
	"github.com/spacemeshos/go-spacemesh/txs"
)

var (
	errNodeHasBadMeshHash   = errors.New("node has different mesh hash from majority")
	errProposalTxMissing    = errors.New("proposal tx not found")
	errProposalTxHdrMissing = errors.New("proposal tx missing header")
	errDuplicateATX         = errors.New("multiple proposals with same ATX")
)

type meshState struct {
	hash  types.Hash32
	count int
}

type proposalMetadata struct {
	ctx        context.Context
	lid        types.LayerID
	proposals  []*types.Proposal
	tids       []types.TransactionID
	tickHeight uint64
	rewards    []types.AnyReward
	optFilter  bool
}

func getProposalMetadata(
	ctx context.Context,
	logger log.Log,
	db *sql.Database,
	atxs *atxsdata.Data,
	cfg Config,
	lid types.LayerID,
	proposals []*types.Proposal,
) (*proposalMetadata, error) {
	var (
		md = &proposalMetadata{
			ctx:       ctx,
			lid:       lid,
			proposals: proposals,
		}
		mtxs       []*types.MeshTransaction
		seen       = make(map[types.TransactionID]struct{})
		meshHashes = make(map[types.Hash32]*meshState)
		err        error
	)
	md.tickHeight, md.rewards, err = rewardInfoAndHeight(cfg, db, atxs, proposals)
	if err != nil {
		return nil, err
	}
	total := 0
	for _, p := range proposals {
		key := p.MeshHash
		cnt := len(p.EligibilityProofs)
		total += cnt
		if _, ok := meshHashes[key]; !ok {
			meshHashes[key] = &meshState{
				hash:  p.MeshHash,
				count: cnt,
			}
		} else {
			meshHashes[key].count += cnt
		}

		for _, tid := range p.TxIDs {
			if _, ok := seen[tid]; ok {
				continue
			}
			mtx, err := transactions.Get(db, tid)
			if err != nil {
				return nil, fmt.Errorf("%w: get proposal tx: %w", errProposalTxMissing, err)
			}
			if mtx.TxHeader == nil {
				return nil, fmt.Errorf(
					"%w: inconsistent state: tx %s is missing header",
					errProposalTxHdrMissing,
					mtx.ID,
				)
			}
			seen[tid] = struct{}{}
			mtxs = append(mtxs, mtx)
		}
	}
	majority := cfg.OptFilterThreshold * total
	var majorityState *meshState
	for _, ms := range meshHashes {
		logger.With().Debug("mesh hash",
			ms.hash,
			log.Int("count", ms.count),
			log.Int("total", total),
			log.Int("threshold", cfg.OptFilterThreshold),
			log.Int("num_proposals", len(proposals)))
		if ms.hash != types.EmptyLayerHash && ms.count*100 >= majority {
			majorityState = ms
		}
	}
	if majorityState == nil {
		logger.With().Info("no consensus on mesh hash. NOT doing optimistic filtering", lid)
	} else {
		ownMeshHash, err := layers.GetAggregatedHash(db, lid.Sub(1))
		if err != nil {
			return nil, fmt.Errorf("get prev mesh hash %w", err)
		}
		if ownMeshHash != majorityState.hash {
			return nil, fmt.Errorf("%w: majority %v, node %v",
				errNodeHasBadMeshHash, majorityState.hash.ShortString(), ownMeshHash.ShortString())
		}
		logger.With().Debug("consensus on mesh hash. doing optimistic filtering",
			lid,
			log.Stringer("mesh_hash", majorityState.hash))
		md.optFilter = true
	}
	if len(mtxs) > 0 {
		var gasLimit uint64
		if !md.optFilter {
			gasLimit = cfg.BlockGasLimit
		}
		blockSeed := types.CalcProposalsHash32(types.ToProposalIDs(md.proposals), nil).Bytes()
		md.tids, err = getBlockTXs(logger, mtxs, blockSeed, gasLimit)
		if err != nil {
			return nil, err
		}
	}
	return md, nil
}

func getBlockTXs(
	logger log.Log,
	mtxs []*types.MeshTransaction,
	blockSeed []byte,
	gasLimit uint64,
) ([]types.TransactionID, error) {
	stateF := func(_ types.Address) (uint64, uint64) {
		return 0, math.MaxUint64
	}
	txCache := txs.NewCache(stateF, logger)
	if err := txCache.BuildFromTXs(mtxs, blockSeed); err != nil {
		return nil, fmt.Errorf("build txs for block: %w", err)
	}
	byAddrAndNonce := txCache.GetMempool(logger)
	if len(byAddrAndNonce) == 0 {
		logger.With().Warning("no feasible txs for block")
		return nil, nil
	}
	candidates := make([]*txs.NanoTX, 0, len(mtxs))
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

func prune(
	logger log.Log,
	tids []types.TransactionID,
	byTid map[types.TransactionID]*txs.NanoTX,
	gasLimit uint64,
) []types.TransactionID {
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
		s = append(s, binary.LittleEndian.Uint64(b[i:min(l, i+numByte)]))
	}
	return s
}

func rewardInfoAndHeight(
	cfg Config,
	db *sql.Database,
	atxs *atxsdata.Data,
	props []*types.Proposal,
) (uint64, []types.AnyReward, error) {
	weights := make(map[types.ATXID]*big.Rat)
	maxHeight := uint64(0)
	for _, p := range props {
		atx := atxs.Get(p.Layer.GetEpoch(), p.AtxID)
		if atx == nil {
			return 0, nil, fmt.Errorf(
				"proposal ATX not found: atx %s proposal %s", p.AtxID.ShortString(), p.ID().String(),
			)
		}
		maxHeight = max(maxHeight, atx.BaseHeight)
		var count uint32
		if p.Ballot.EpochData != nil {
			count = p.Ballot.EpochData.EligibilityCount
		} else {
			ref, err := ballots.Get(db, p.RefBallot)
			if err != nil {
				return 0, nil, fmt.Errorf("get ballot %s: %w", p.RefBallot.String(), err)
			}
			if ref.EpochData == nil {
				return 0, nil, fmt.Errorf("corrupted data: ref ballot %s with empty epoch data", p.RefBallot.String())
			}
			count = ref.EpochData.EligibilityCount
		}
		if _, ok := weights[p.AtxID]; !ok {
			weight := new(big.Rat).SetFrac(
				new(big.Int).SetUint64(atx.Weight),
				new(big.Int).SetUint64(uint64(count)),
			)
			weight.Mul(weight, new(big.Rat).SetUint64(uint64(len(p.Ballot.EligibilityProofs))))
			weights[p.AtxID] = weight
		} else {
			return 0, nil, fmt.Errorf(
				"%w: multiple proposals with the same ATX atx %v proposal %v",
				errDuplicateATX, p.AtxID, p.ID(),
			)
		}
		events.ReportProposal(events.ProposalIncluded, p)
	}
	atxids := maps.Keys(weights)
	// keys in order so that everyone generates same block
	sort.Slice(atxids, func(i, j int) bool {
		return bytes.Compare(atxids[i].Bytes(), atxids[j].Bytes()) < 0
	})
	rewards := make([]types.AnyReward, 0, len(weights))
	for _, id := range atxids {
		weight := weights[id]
		rewards = append(rewards, types.AnyReward{
			AtxID: id,
			Weight: types.RatNum{
				Num:   weight.Num().Uint64(),
				Denom: weight.Denom().Uint64(),
			},
		})
	}
	return maxHeight, rewards, nil
}
