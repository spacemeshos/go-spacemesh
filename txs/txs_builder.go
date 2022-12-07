package txs

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"math"
	"math/rand"
	"sort"
	"time"

	"github.com/seehuhn/mt19937"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/common/util"
	"github.com/spacemeshos/go-spacemesh/datastore"
	"github.com/spacemeshos/go-spacemesh/events"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/proposals"
	txtypes "github.com/spacemeshos/go-spacemesh/txs/types"
)

var (
	errNodeHasBadMeshHash = errors.New("node has different mesh hash from majority")
	errInvalidATXID       = errors.New("proposal ATXID invalid")
)

type blockMetadata struct {
	candidates     []*txtypes.NanoTX
	byAddrAndNonce map[types.Address][]*txtypes.NanoTX
	byTid          map[types.TransactionID]*txtypes.NanoTX
}

type meshState struct {
	hash  types.Hash32
	count int
}

type proposalMetadata struct {
	lid        types.LayerID
	size       int
	meshHashes map[types.Hash32]*meshState
	mtxs       []*types.MeshTransaction
	optFilter  bool
}

func extractProposalMetadata(
	logger log.Log,
	cfg CSConfig,
	lid types.LayerID,
	proposals []*types.Proposal,
	gtx txGetter,
) (*proposalMetadata, error) {
	var (
		seen       = make(map[types.TransactionID]struct{})
		mtxs       = make([]*types.MeshTransaction, 0, len(proposals)*cfg.NumTXsPerProposal)
		meshHashes = make(map[types.Hash32]*meshState)
	)
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
			mtx, err := gtx.GetMeshTransaction(tid)
			if err != nil {
				logger.With().Error("failed to find proposal tx", p.LayerIndex, p.ID(), tid, log.Err(err))
				return nil, fmt.Errorf("get proposal tx: %w", err)
			}
			if mtx.TxHeader == nil {
				return nil, fmt.Errorf("inconsistent state: tx %s is missing header", mtx.ID)
			}
			seen[tid] = struct{}{}
			mtxs = append(mtxs, mtx)
		}
	}
	logger.With().Info("extracted proposals metadata",
		lid,
		log.Int("num_mesh_hash", len(meshHashes)),
		log.Int("num_txs", len(mtxs)))
	return &proposalMetadata{lid: lid, size: len(proposals), mtxs: mtxs, meshHashes: meshHashes}, nil
}

func getMajorityState(logger log.Log, meshHashes map[types.Hash32]*meshState, numProposals, threshold int) *meshState {
	for _, ms := range meshHashes {
		logger.With().Debug("mesh hash",
			ms.hash,
			log.Int("count", ms.count),
			log.Int("threshold", threshold),
			log.Int("num_proposals", numProposals))
		if ms.hash != types.EmptyLayerHash && ms.count*100 >= numProposals*threshold {
			return ms
		}
	}
	return nil
}

// returns true if there is a consensus on mesh state across proposals.
func checkStateConsensus(
	logger log.Log,
	cfg CSConfig,
	lid types.LayerID,
	proposals []*types.Proposal,
	ownMeshHash types.Hash32,
	gtx txGetter,
) (*proposalMetadata, error) {
	md, err := extractProposalMetadata(logger, cfg, lid, proposals, gtx)
	if err != nil {
		return nil, err
	}

	ms := getMajorityState(logger, md.meshHashes, md.size, cfg.OptFilterThreshold)
	if ms == nil {
		logger.With().Warning("no consensus on mesh hash. NOT doing optimistic filtering", lid)
		return md, nil
	}

	if ownMeshHash != ms.hash {
		logger.With().Error("node mesh hash differ from majority",
			lid,
			log.Stringer("majority_hash", ms.hash),
			log.Stringer("node_hash", ownMeshHash))
		return nil, errNodeHasBadMeshHash
	}
	md.optFilter = true
	logger.With().Info("consensus on mesh hash. doing optimistic filtering",
		lid,
		log.Stringer("mesh_hash", ms.hash))
	return md, nil
}

// uses a DB-less cache to organize the transactions into a list of transactions that are in nonce order
// with respect to its principal.
// e.g.
// input:  [(addr-0, nonce-9), (addr-1, nonce-4), (addr-2, nonce-7), (addr-0, nonce-8), (addr-1, nonce-3)]
// output: [(addr-0, nonce-8), (addr-0, nonce-9), (addr-1, nonce-3), (addr-1, nonce-4), (addr-2, nonce-7)]
func orderTXs(logger log.Log, pmd *proposalMetadata, blockSeed []byte) (*blockMetadata, error) {
	stateF := func(_ types.Address) (uint64, uint64) {
		return 0, math.MaxUint64
	}
	// this cache is used for building the set of transactions in a block.
	txCache := &cache{
		logger:    logger,
		stateF:    stateF,
		pending:   make(map[types.Address]*accountCache),
		cachedTXs: make(map[types.TransactionID]*txtypes.NanoTX),
	}
	if err := txCache.BuildFromTXs(pmd.mtxs, blockSeed); err != nil {
		return nil, err
	}
	byAddrAndNonce := txCache.GetMempool(logger)
	ntxs := make([]*txtypes.NanoTX, 0, len(pmd.mtxs))
	byTid := make(map[types.TransactionID]*txtypes.NanoTX)
	for _, acctTXs := range byAddrAndNonce {
		ntxs = append(ntxs, acctTXs...)
		for _, ntx := range acctTXs {
			byTid[ntx.ID] = ntx
		}
	}
	return &blockMetadata{candidates: ntxs, byAddrAndNonce: byAddrAndNonce, byTid: byTid}, nil
}

func getBlockTXs(logger log.Log, pmd *proposalMetadata, blockSeed []byte, gasLimit uint64) ([]types.TransactionID, error) {
	bmd, err := orderTXs(logger, pmd, blockSeed)
	if err != nil {
		logger.With().Error("failed to build txs cache for block", log.Err(err))
		return nil, err
	}
	if len(bmd.candidates) == 0 {
		logger.With().Warning("no feasible txs for block")
		return nil, nil
	}

	sort.Slice(bmd.candidates, func(i, j int) bool { return bmd.candidates[i].ID.Compare(bmd.candidates[j].ID) })
	// initialize a Mersenne Twister with the block seed and use it as a source of randomness for
	// a Fisher-Yates shuffle of the sorted transaction IDs.
	mt := mt19937.New()
	mt.SeedFromSlice(toUint64Slice(blockSeed))
	rng := rand.New(mt)
	ordered := shuffleWithNonceOrder(logger, rng, len(bmd.candidates), bmd.candidates, bmd.byAddrAndNonce)
	if gasLimit > 0 {
		ordered = prune(logger, ordered, bmd.byTid, gasLimit)
	}
	return ordered, nil
}

func getProposalTXs(logger log.Log, numTXs int, predictedBlock []*txtypes.NanoTX, byAddrAndNonce map[types.Address][]*txtypes.NanoTX) []types.TransactionID {
	if len(predictedBlock) <= numTXs {
		result := make([]types.TransactionID, 0, len(predictedBlock))
		for _, ntx := range predictedBlock {
			result = append(result, ntx.ID)
		}
		return result
	}
	// randomly select transactions from the predicted block.
	rng := rand.New(rand.NewSource(time.Now().UnixNano()))
	return shuffleWithNonceOrder(logger, rng, numTXs, predictedBlock, byAddrAndNonce)
}

// perform a Fisher-Yates shuffle on the transactions. note that after shuffling, the original list of transactions
// are no longer in nonce order within the same principal. we simply check which principal occupies the spot after
// the shuffle and retrieve their transactions in nonce order.
func shuffleWithNonceOrder(
	logger log.Log,
	rng *rand.Rand,
	numTXs int,
	ntxs []*txtypes.NanoTX,
	byAddrAndNonce map[types.Address][]*txtypes.NanoTX,
) []types.TransactionID {
	rng.Shuffle(len(ntxs), func(i, j int) { ntxs[i], ntxs[j] = ntxs[j], ntxs[i] })
	total := util.Min(len(ntxs), numTXs)
	result := make([]types.TransactionID, 0, total)
	packed := make(map[types.Address][]uint64)
	for _, ntx := range ntxs[:total] {
		// if a spot is taken by a principal, we add its TX for the next eligible nonce
		p := ntx.Principal
		if _, ok := byAddrAndNonce[p]; !ok {
			logger.With().Fatal("principal missing", p)
		}
		if len(byAddrAndNonce[p]) == 0 {
			logger.With().Fatal("txs missing", p)
		}
		toAdd := byAddrAndNonce[p][0]
		result = append(result, toAdd.ID)
		if _, ok := packed[p]; !ok {
			packed[p] = []uint64{toAdd.Nonce.Counter, toAdd.Nonce.Counter}
		} else {
			packed[p][1] = toAdd.Nonce.Counter
		}
		if len(byAddrAndNonce[p]) == 1 {
			delete(byAddrAndNonce, p)
		} else {
			byAddrAndNonce[p] = byAddrAndNonce[p][1:]
		}
	}
	logger.With().Debug("packed txs", log.Array("ranges", log.ArrayMarshalerFunc(func(encoder log.ArrayEncoder) error {
		for addr, nonces := range packed {
			_ = encoder.AppendObject(log.ObjectMarshallerFunc(func(encoder log.ObjectEncoder) error {
				encoder.AddString("addr", addr.String())
				encoder.AddUint64("from", nonces[0])
				encoder.AddUint64("to", nonces[1])
				return nil
			}))
		}
		return nil
	})))
	return result
}

func prune(logger log.Log, tids []types.TransactionID, byTid map[types.TransactionID]*txtypes.NanoTX, gasLimit uint64) []types.TransactionID {
	if len(tids) == 0 {
		return nil
	}
	var (
		gasRemaining = gasLimit
		idx          int
		tid          types.TransactionID
	)
	for idx, tid = range tids {
		if gasRemaining < minTXGas {
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

func extractCoinbasesAndHeight(logger log.Log, cdb *datastore.CachedDB, props []*types.Proposal, cfg CSConfig) (uint64, []types.AnyReward, error) {
	weights := make(map[types.Address]util.Weight)
	coinbases := make([]types.Address, 0, len(props))
	max := uint64(0)
	for _, p := range props {
		if p.AtxID == *types.EmptyATXID {
			// this proposal would not have been validated
			return 0, nil, fmt.Errorf("%w: invalid ATX for proposal %v", errInvalidATXID, p.ID())
		}
		atx, err := cdb.GetAtxHeader(p.AtxID)
		if atx.BaseTickHeight > max {
			max = atx.BaseTickHeight
		}
		if err != nil {
			return 0, nil, fmt.Errorf("get ATX for proposal %v: %w", p.ID(), err)
		}
		ballot := &p.Ballot
		weightPer, err := proposals.ComputeWeightPerEligibility(cdb, ballot, cfg.LayerSize, cfg.LayersPerEpoch)
		if err != nil {
			return 0, nil, fmt.Errorf("compute weight per eligibility %v: %w", p.ID(), err)
		}
		logger.With().Debug("weight per eligibility", p.ID(), log.Stringer("weight_per", weightPer))
		actual := weightPer.Mul(util.WeightFromUint64(uint64(len(ballot.EligibilityProofs))))
		if _, ok := weights[atx.Coinbase]; !ok {
			weights[atx.Coinbase] = actual
			coinbases = append(coinbases, atx.Coinbase)
		} else {
			weights[atx.Coinbase].Add(actual)
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
