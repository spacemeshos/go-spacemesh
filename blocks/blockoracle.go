package blocks

import (
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/database"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/proposals"
	"github.com/spacemeshos/go-spacemesh/system"
)

type activationDB interface {
	GetNodeAtxIDForEpoch(nodeID types.NodeID, targetEpoch types.EpochID) (types.ATXID, error)
	GetAtxHeader(types.ATXID) (*types.ActivationTxHeader, error)
	GetEpochWeight(types.EpochID) (uint64, []types.ATXID, error)
}

type vrfSigner interface {
	Sign(msg []byte) []byte
}

// ErrMinerHasNoATXInPreviousEpoch is returned when miner has no ATXs in previous epoch.
var ErrMinerHasNoATXInPreviousEpoch = errors.New("miner has no ATX in previous epoch")

// Oracle is the oracle that provides block eligibility proofs for the miner.
type Oracle struct {
	committeeSize  uint32
	layersPerEpoch uint32
	atxDB          activationDB
	beaconProvider system.BeaconGetter
	vrfSigner      vrfSigner
	nodeID         types.NodeID
	isSynced       func() bool
	log            log.Log

	mu                sync.Mutex
	proofsEpoch       types.EpochID
	epochAtxs         []types.ATXID
	eligibilityProofs map[types.LayerID][]types.BlockEligibilityProof
	atx               *types.ActivationTxHeader
}

// NewMinerBlockOracle returns a new Oracle.
func NewMinerBlockOracle(committeeSize uint32, layersPerEpoch uint32, atxDB activationDB, beaconProvider system.BeaconGetter, vrfSigner vrfSigner, nodeID types.NodeID, isSynced func() bool, log log.Log) *Oracle {
	return &Oracle{
		committeeSize:  committeeSize,
		layersPerEpoch: layersPerEpoch,
		atxDB:          atxDB,
		beaconProvider: beaconProvider,
		vrfSigner:      vrfSigner,
		nodeID:         nodeID,
		isSynced:       isSynced,
		log:            log,
	}
}

// BlockEligible returns the ATXID, list of block eligibility proofs for the given layer and the active set of the epoch.
// It caches proofs for a single epoch and only refreshes the cache if eligibility is queried for a different epoch.
func (bo *Oracle) BlockEligible(layerID types.LayerID) (types.ATXID, []types.BlockEligibilityProof, []types.ATXID, error) {
	bo.mu.Lock()
	defer bo.mu.Unlock()

	epoch := layerID.GetEpoch()
	logger := bo.log.WithFields(layerID, epoch, bo.proofsEpoch)

	if epoch.IsGenesis() {
		logger.Error("asked for block eligibility in genesis epoch, cannot create blocks here",
			layerID, epoch)
		return *types.EmptyATXID, nil, nil, nil
	}

	if !bo.isSynced() {
		return *types.EmptyATXID, nil, nil, fmt.Errorf("cannot calc eligibility, not synced yet")
	}

	logger.Info("asked for block eligibility")

	var proofs []types.BlockEligibilityProof
	if bo.proofsEpoch == epoch { // use the cached value
		proofs = bo.eligibilityProofs[layerID]
		logger.With().Info("got cached eligibility for blocks", log.Int("num_blocks", len(proofs)))
		return bo.atx.ID(), proofs, bo.epochAtxs, nil
	}

	// calculate the proof
	atx, err := bo.getValidAtxForEpoch(epoch)
	if err != nil {
		if errors.Is(err, database.ErrNotFound) {
			return *types.EmptyATXID, nil, nil, ErrMinerHasNoATXInPreviousEpoch
		}
		return *types.EmptyATXID, nil, nil, fmt.Errorf("failed to get valid atx for node for target epoch %d: %w", epoch, err)
	}

	newProofs, activeSet, err := bo.calcEligibilityProofs(atx.GetWeight(), epoch)
	if err != nil {
		logger.With().Error("failed to calculate eligibility proofs", log.Err(err))
		return *types.EmptyATXID, nil, nil, err
	}

	bo.proofsEpoch = epoch
	bo.atx = atx
	bo.epochAtxs = activeSet
	bo.eligibilityProofs = newProofs

	proofs = bo.eligibilityProofs[layerID]
	logger.With().Info("got eligibility for blocks", log.Int("num_blocks", len(proofs)))

	return bo.atx.ID(), proofs, bo.epochAtxs, nil
}

// calcEligibilityProofs calculates the eligibility proofs of blocks for the miner in the given epoch
// and returns the proofs along with the epoch's active set.
func (bo *Oracle) calcEligibilityProofs(weight uint64, epoch types.EpochID) (map[types.LayerID][]types.BlockEligibilityProof, []types.ATXID, error) {
	logger := bo.log.WithFields(epoch)

	beacon, err := bo.beaconProvider.GetBeacon(epoch)
	if err != nil {
		logger.With().Error("Failed to get beacon", log.Err(err))
		return nil, nil, fmt.Errorf("get beacon: %w", err)
	}

	beaconDbgStr := types.BytesToHash(beacon).ShortString()
	logger.With().Info("block oracle got beacon", log.String("beacon", beaconDbgStr))

	// get the previous epoch's total weight
	totalWeight, activeSet, err := bo.atxDB.GetEpochWeight(epoch)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to get epoch %v weight: %w", epoch, err)
	}

	logger = logger.WithFields(log.Uint64("weight", weight), log.Uint64("total_weight", totalWeight))
	logger.Info("calculating eligibility")

	numberOfEligibleBlocks, err := proposals.GetNumEligibleSlots(weight, totalWeight, bo.committeeSize, bo.layersPerEpoch)
	if err != nil {
		logger.With().Error("failed to get number of eligible blocks", log.Err(err))
		return nil, nil, fmt.Errorf("oracle get num slots: %w", err)
	}

	eligibilityProofs := map[types.LayerID][]types.BlockEligibilityProof{}
	for counter := uint32(0); counter < numberOfEligibleBlocks; counter++ {
		message, err := proposals.SerializeVRFMessage(beacon, epoch, counter)
		if err != nil {
			return nil, nil, fmt.Errorf("oracle serialize vrf: %w", err)
		}
		vrfSig := bo.vrfSigner.Sign(message)

		logger.Debug("signed vrf message, beacon %v, epoch %v, counter: %v, vrfSig: %v",
			beaconDbgStr, epoch, counter, types.BytesToHash(vrfSig).ShortString())

		eligibleLayer := proposals.CalcEligibleLayer(epoch, bo.layersPerEpoch, vrfSig)
		eligibilityProofs[eligibleLayer] = append(eligibilityProofs[eligibleLayer], types.BlockEligibilityProof{
			J:   counter,
			Sig: vrfSig,
		})
	}

	// Sort the layer map so we can print the layer data in order
	keys := make([]types.LayerID, len(eligibilityProofs))
	i := 0
	for k := range eligibilityProofs {
		keys[i] = k
		i++
	}
	sort.Slice(keys, func(i, j int) bool {
		return keys[i].Before(keys[j])
	})

	// Pretty-print the number of blocks per eligible layer
	var strs []string
	for k := range keys {
		strs = append(strs, fmt.Sprintf("Layer %s: %d", keys[k].String(), len(eligibilityProofs[keys[k]])))
	}

	logger.With().Info("block eligibility calculated",
		log.Uint32("total_num_blocks", numberOfEligibleBlocks),
		log.Int("num_layers_eligible", len(eligibilityProofs)),
		log.String("layers_and_num_blocks", strings.Join(strs, ", ")))
	return eligibilityProofs, activeSet, nil
}

func (bo *Oracle) getValidAtxForEpoch(validForEpoch types.EpochID) (*types.ActivationTxHeader, error) {
	atxID, err := bo.getATXIDForEpoch(validForEpoch - 1)
	if err != nil {
		return nil, fmt.Errorf("get atx id for target epoch %v: %w", validForEpoch, err)
	}

	atx, err := bo.atxDB.GetAtxHeader(atxID)
	if err != nil {
		bo.log.With().Error("getting atx failed", log.Err(err))
		return nil, fmt.Errorf("get ATX header: %w", err)
	}

	return atx, nil
}

func (bo *Oracle) getATXIDForEpoch(targetEpoch types.EpochID) (types.ATXID, error) {
	latestATXID, err := bo.atxDB.GetNodeAtxIDForEpoch(bo.nodeID, targetEpoch)
	if err != nil {
		bo.log.With().Warning("did not find atx ids for node",
			log.FieldNamed("atx_node_id", bo.nodeID),
			log.Err(err))
		return types.ATXID{}, fmt.Errorf("get ATX ID from DB: %w", err)
	}
	bo.log.With().Info("latest atx id found", latestATXID)
	return latestATXID, nil
}
