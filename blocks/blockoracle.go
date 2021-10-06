package blocks

import (
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/common/util"
	"github.com/spacemeshos/go-spacemesh/database"
	"github.com/spacemeshos/go-spacemesh/log"
)

type activationDB interface {
	GetNodeAtxIDForEpoch(nodeID types.NodeID, targetEpoch types.EpochID) (types.ATXID, error)
	GetAtxHeader(types.ATXID) (*types.ActivationTxHeader, error)
	GetEpochWeight(types.EpochID) (uint64, []types.ATXID, error)
}

type vrfSigner interface {
	Sign(msg []byte) []byte
}

// DefaultProofsEpoch is set such that it will never equal the current epoch
const DefaultProofsEpoch = ^types.EpochID(0)

// ErrMinerHasNoATXInPreviousEpoch is returned when miner has no ATXs in previous epoch.
var ErrMinerHasNoATXInPreviousEpoch = errors.New("miner has no ATX in previous epoch")

// Oracle is the oracle that provides block eligibility proofs for the miner.
type Oracle struct {
	committeeSize  uint32
	layersPerEpoch uint32
	atxDB          activationDB
	beaconProvider BeaconGetter
	vrfSigner      vrfSigner
	nodeID         types.NodeID

	proofsEpoch       types.EpochID
	epochAtxs         []types.ATXID
	eligibilityProofs map[types.LayerID][]types.BlockEligibilityProof
	atx               *types.ActivationTxHeader
	isSynced          func() bool
	eligibilityMutex  sync.RWMutex
	log               log.Log
}

// NewMinerBlockOracle returns a new Oracle.
func NewMinerBlockOracle(committeeSize uint32, layersPerEpoch uint32, atxDB activationDB, beaconProvider BeaconGetter, vrfSigner vrfSigner, nodeID types.NodeID, isSynced func() bool, log log.Log) *Oracle {
	return &Oracle{
		committeeSize:  committeeSize,
		layersPerEpoch: layersPerEpoch,
		atxDB:          atxDB,
		beaconProvider: beaconProvider,
		vrfSigner:      vrfSigner,
		nodeID:         nodeID,
		proofsEpoch:    DefaultProofsEpoch,
		isSynced:       isSynced,
		log:            log,
	}
}

// BlockEligible returns the ATXID and list of block eligibility proofs for the given layer. It caches proofs for a
// single epoch and only refreshes the cache if eligibility is queried for a different epoch.
func (bo *Oracle) BlockEligible(layerID types.LayerID) (types.ATXID, []types.BlockEligibilityProof, []types.ATXID, error) {
	if !bo.isSynced() {
		return types.ATXID{}, nil, nil, fmt.Errorf("cannot calc eligibility, not synced yet")
	}

	epochNumber := layerID.GetEpoch()
	atx, err := bo.getValidAtxForEpoch(epochNumber)
	if err != nil {
		if errors.Is(err, database.ErrNotFound) {
			return types.ATXID{}, nil, nil, ErrMinerHasNoATXInPreviousEpoch
		}
		return types.ATXID{}, nil, nil, fmt.Errorf("failed to get latest atx for node in epoch %d: %w", epochNumber, err)
	}
	bo.atx = atx

	var cachedEpochDescription log.Field
	if bo.proofsEpoch == DefaultProofsEpoch {
		cachedEpochDescription = log.Int("cached_epoch_id", -1)
	} else {
		cachedEpochDescription = log.FieldNamed("cached_epoch_id", bo.proofsEpoch.Field())
	}
	bo.log.With().Info("asked for block eligibility", layerID, epochNumber, cachedEpochDescription)
	if epochNumber.IsGenesis() {
		bo.log.With().Error("asked for block eligibility in genesis epoch, cannot create blocks here",
			layerID, epochNumber, cachedEpochDescription)
		return *types.EmptyATXID, nil, nil, nil
	}
	var proofs []types.BlockEligibilityProof
	bo.eligibilityMutex.RLock()
	if bo.proofsEpoch != epochNumber {
		bo.eligibilityMutex.RUnlock()
		newProofs, err := bo.calcEligibilityProofs(epochNumber)
		proofs = newProofs[layerID]
		if err != nil {
			bo.log.With().Error("failed to calculate eligibility proofs", layerID, epochNumber, log.Err(err))
			return *types.EmptyATXID, nil, nil, err
		}
	} else {
		proofs = bo.eligibilityProofs[layerID]
		bo.eligibilityMutex.RUnlock()
	}
	bo.log.With().Info("got eligibility for blocks",
		bo.nodeID, layerID, layerID.GetEpoch(),
		log.Int("num_blocks", len(proofs)))

	return bo.atx.ID(), proofs, bo.epochAtxs, nil
}

func (bo *Oracle) calcEligibilityProofs(epochNumber types.EpochID) (map[types.LayerID][]types.BlockEligibilityProof, error) {
	epochBeacon, err := bo.beaconProvider.GetBeacon(epochNumber)
	if err != nil {
		bo.log.With().Error("Failed to get beacon",
			log.Uint64("epoch_id", uint64(epochNumber)),
			log.Err(err))

		return nil, fmt.Errorf("get beacon: %w", err)
	}

	beaconDbgStr := types.BytesToHash(epochBeacon).ShortString()
	bo.log.With().Info("got beacon",
		log.Uint64("epoch_id", uint64(epochNumber)),
		log.String("epoch_beacon", beaconDbgStr))

	// get the previous epoch's total weight
	totalWeight, activeSet, err := bo.atxDB.GetEpochWeight(epochNumber)
	if err != nil {
		return nil, fmt.Errorf("failed to get epoch %v weight: %w", epochNumber, err)
	}

	bo.log.With().Info("calculating eligibility",
		epochNumber,
		log.Uint64("total_weight", totalWeight))
	bo.log.With().Debug("calculating eligibility",
		epochNumber,
		log.String("epoch_beacon", beaconDbgStr))

	weight := bo.atx.GetWeight()
	numberOfEligibleBlocks, err := getNumberOfEligibleBlocks(weight, totalWeight, bo.committeeSize, bo.layersPerEpoch)
	if err != nil {
		bo.log.With().Error("failed to get number of eligible blocks", log.Err(err))
		return nil, err
	}

	eligibilityProofs := map[types.LayerID][]types.BlockEligibilityProof{}
	for counter := uint32(0); counter < numberOfEligibleBlocks; counter++ {
		message, err := serializeVRFMessage(epochBeacon, epochNumber, counter)
		if err != nil {
			return nil, err
		}
		vrfSig := bo.vrfSigner.Sign(message)

		bo.log.Debug("signed vrf message, beacon %v, epoch %v, counter: %v, vrfSig: %v",
			types.BytesToHash(epochBeacon).ShortString(), epochNumber, counter, types.BytesToHash(vrfSig).ShortString())

		eligibleLayer := calcEligibleLayer(epochNumber, bo.layersPerEpoch, vrfSig)
		eligibilityProofs[eligibleLayer] = append(eligibilityProofs[eligibleLayer], types.BlockEligibilityProof{
			J:   counter,
			Sig: vrfSig,
		})
	}

	bo.eligibilityMutex.Lock()
	bo.epochAtxs = activeSet
	bo.proofsEpoch = epochNumber
	bo.eligibilityProofs = eligibilityProofs
	bo.eligibilityMutex.Unlock()

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

	bo.log.With().Info("block eligibility calculated",
		epochNumber,
		log.Uint32("total_num_blocks", numberOfEligibleBlocks),
		log.Int("num_layers_eligible", len(eligibilityProofs)),
		log.String("layers_and_num_blocks", strings.Join(strs, ", ")))
	return eligibilityProofs, nil
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

func calcEligibleLayer(epochNumber types.EpochID, layersPerEpoch uint32, vrfSig []byte) types.LayerID {
	vrfInteger := util.BytesToUint64(vrfSig)
	eligibleLayerOffset := vrfInteger % uint64(layersPerEpoch)
	return epochNumber.FirstLayer().Add(uint32(eligibleLayerOffset))
}

func getNumberOfEligibleBlocks(weight, totalWeight uint64, committeeSize uint32, layersPerEpoch uint32) (uint32, error) {
	if totalWeight == 0 {
		return 0, errors.New("zero total weight not allowed")
	}
	numberOfEligibleBlocks := weight * uint64(committeeSize) * uint64(layersPerEpoch) / totalWeight // TODO: ensure no overflow
	if numberOfEligibleBlocks == 0 {
		numberOfEligibleBlocks = 1
	}
	return uint32(numberOfEligibleBlocks), nil
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

type vrfMessage struct {
	EpochBeacon []byte
	EpochNumber types.EpochID
	Counter     uint32
}

func serializeVRFMessage(epochBeacon []byte, epochNumber types.EpochID, counter uint32) ([]byte, error) {
	m := vrfMessage{
		EpochBeacon: epochBeacon,
		EpochNumber: epochNumber,
		Counter:     counter,
	}
	serialized, err := types.InterfaceToBytes(&m)
	if err != nil {
		return nil, fmt.Errorf("failed to serialize vrf message: %v", err)
	}
	return serialized, nil
}

// GetEligibleLayers returns a list of layers in which the miner is eligible for at least one block. The list is
// returned in arbitrary order.
func (bo *Oracle) GetEligibleLayers() []types.LayerID {
	bo.eligibilityMutex.RLock()
	layers := make([]types.LayerID, 0, len(bo.eligibilityProofs))
	for layer := range bo.eligibilityProofs {
		layers = append(layers, layer)
	}
	bo.eligibilityMutex.RUnlock()
	return layers
}
