package blocks

import (
	"encoding/binary"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"

	"github.com/spacemeshos/sha256-simd"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/log"
)

type activationDB interface {
	GetNodeAtxIDForEpoch(types.NodeID, types.EpochID) (types.ATXID, error)
	GetAtxHeader(types.ATXID) (*types.ActivationTxHeader, error)
	GetIdentity(edID string) (types.NodeID, error)
	GetEpochAtxs(types.EpochID) []types.ATXID
}

type vrfSigner interface {
	Sign(msg []byte) ([]byte, error)
}

// DefaultProofsEpoch is set such that it will never equal the current epoch
const DefaultProofsEpoch = ^types.EpochID(0)

// Oracle is the oracle that provides block eligibility proofs for the miner.
type Oracle struct {
	committeeSize        uint32
	genesisActiveSetSize uint32
	layersPerEpoch       uint16
	atxDB                activationDB
	beaconProvider       *EpochBeaconProvider
	vrfSigner            vrfSigner
	nodeID               types.NodeID

	proofsEpoch       types.EpochID
	epochAtxs         []types.ATXID
	eligibilityProofs map[types.LayerID][]types.BlockEligibilityProof
	atxID             types.ATXID
	isSynced          func() bool
	eligibilityMutex  sync.RWMutex
	log               log.Log
}

// NewMinerBlockOracle returns a new Oracle.
func NewMinerBlockOracle(committeeSize uint32, genesisActiveSetSize uint32, layersPerEpoch uint16, atxDB activationDB, beaconProvider *EpochBeaconProvider, vrfSigner vrfSigner, nodeID types.NodeID, isSynced func() bool, log log.Log) *Oracle {
	return &Oracle{
		committeeSize:        committeeSize,
		genesisActiveSetSize: genesisActiveSetSize,
		layersPerEpoch:       layersPerEpoch,
		atxDB:                atxDB,
		beaconProvider:       beaconProvider,
		vrfSigner:            vrfSigner,
		nodeID:               nodeID,
		proofsEpoch:          DefaultProofsEpoch,
		isSynced:             isSynced,
		log:                  log,
	}
}

// BlockEligible returns the ATXID and list of block eligibility proofs for the given layer. It caches proofs for a
// single epoch and only refreshes the cache if eligibility is queried for a different epoch.
func (bo *Oracle) BlockEligible(layerID types.LayerID) (types.ATXID, []types.BlockEligibilityProof, []types.ATXID, error) {
	if !bo.isSynced() {
		return types.ATXID{}, nil, nil, fmt.Errorf("cannot calc eligibility, not synced yet")
	}

	epochNumber := layerID.GetEpoch()
	var cachedEpochDescription log.Field
	if bo.proofsEpoch == DefaultProofsEpoch {
		cachedEpochDescription = log.String("cached_epoch_id", "(none)")
	} else {
		cachedEpochDescription = log.FieldNamed("cached_epoch_id", bo.proofsEpoch.Field())
	}
	bo.log.With().Info("asked for eligibility",
		log.FieldNamed("epoch_id", epochNumber),
		cachedEpochDescription)
	if epochNumber.IsGenesis() {
		bo.log.Info("asked for eligibility for genesis epoch, cannot create blocks here")
		return *types.EmptyATXID, nil, nil, nil
	}

	if bo.proofsEpoch != epochNumber {
		if err := bo.calcEligibilityProofs(epochNumber); err != nil {
			bo.log.With().Error("failed to calculate eligibility proofs", epochNumber, log.Err(err))
			return *types.EmptyATXID, nil, nil, err
		}
	}

	bo.eligibilityMutex.RLock()
	proofs := bo.eligibilityProofs[layerID]
	activeSet := bo.epochAtxs
	bo.eligibilityMutex.RUnlock()

	bo.log.With().Info("got eligibility for blocks in layer",
		bo.nodeID,
		layerID,
		log.Int("active_set_size", len(activeSet)),
		log.Int("num_blocks", len(proofs)))

	return bo.atxID, proofs, activeSet, nil
}

func (bo *Oracle) calcEligibilityProofs(epochNumber types.EpochID) error {
	epochBeacon := bo.beaconProvider.GetBeacon(epochNumber)

	// get the previous epochs total ATXs
	activeSet := bo.atxDB.GetEpochAtxs(epochNumber - 1)
	activeSetSize := uint32(len(activeSet))
	atx, err := bo.getValidAtxForEpoch(epochNumber)
	if err != nil {
		if !epochNumber.IsGenesis() {
			return fmt.Errorf("failed to get latest atx for node in epoch %d: %v", epochNumber, err)
		}
	} else {
		bo.atxID = atx.ID()
	}
	bo.log.With().Info("calculating eligibility",
		epochNumber,
		log.Uint32("active_set_size", activeSetSize))
	bo.log.With().Debug("calculating eligibility",
		epochNumber,
		log.String("epoch_beacon", fmt.Sprint(epochBeacon)))

	if epochNumber.IsGenesis() {
		bo.log.With().Info("genesis epoch detected, using GenesisActiveSetSize",
			log.Uint32("active_set_size", activeSetSize))
	}

	numberOfEligibleBlocks, err := getNumberOfEligibleBlocks(activeSetSize, bo.committeeSize, bo.layersPerEpoch)
	if err != nil {
		bo.log.With().Error("failed to get number of eligible blocks", log.Err(err))
		return err
	}

	bo.eligibilityMutex.Lock()
	bo.eligibilityProofs = map[types.LayerID][]types.BlockEligibilityProof{}
	bo.eligibilityMutex.Unlock()
	for counter := uint32(0); counter < numberOfEligibleBlocks; counter++ {
		message := serializeVRFMessage(epochBeacon, epochNumber, counter)
		vrfSig, err := bo.vrfSigner.Sign(message)
		if err != nil {
			bo.log.With().Error("could not sign message", log.Err(err))
			return err
		}
		vrfHash := sha256.Sum256(vrfSig)
		eligibleLayer := calcEligibleLayer(epochNumber, bo.layersPerEpoch, vrfHash)
		bo.eligibilityMutex.Lock()
		bo.eligibilityProofs[eligibleLayer] = append(bo.eligibilityProofs[eligibleLayer], types.BlockEligibilityProof{
			J:   counter,
			Sig: vrfSig,
		})
		bo.eligibilityMutex.Unlock()
	}
	bo.eligibilityMutex.Lock()
	bo.epochAtxs = activeSet
	bo.proofsEpoch = epochNumber
	bo.eligibilityMutex.Unlock()

	bo.eligibilityMutex.RLock()

	// Sort the layer map so we can print the layer data in order
	keys := make([]types.LayerID, len(bo.eligibilityProofs))
	i := 0
	for k := range bo.eligibilityProofs {
		keys[i] = k
		i++
	}
	sort.Slice(keys, func(i, j int) bool {
		return uint64(keys[i]) < uint64(keys[j])
	})

	// Pretty-print the number of blocks per eligible layer
	var strs []string
	for k := range keys {
		strs = append(strs, fmt.Sprintf("Layer %d: %d", keys[k], len(bo.eligibilityProofs[keys[k]])))
	}

	bo.log.With().Info("eligibility for blocks in epoch",
		bo.nodeID,
		epochNumber,
		log.Uint32("total_num_blocks", numberOfEligibleBlocks),
		log.Int("active_set", len(activeSet)),
		log.Int("num_layers_eligible", len(bo.eligibilityProofs)),
		log.String("layers_and_num_blocks", strings.Join(strs, ", ")))
	bo.eligibilityMutex.RUnlock()
	return nil
}

func (bo *Oracle) getValidAtxForEpoch(validForEpoch types.EpochID) (*types.ActivationTxHeader, error) {
	atxID, err := bo.getATXIDForEpoch(validForEpoch - 1)
	if err != nil {
		return nil, fmt.Errorf("failed to get atx id for target epoch %v: %v", validForEpoch, err)
	}
	atx, err := bo.atxDB.GetAtxHeader(atxID)
	if err != nil {
		bo.log.With().Error("getting atx failed", log.Err(err))
		return nil, err
	}
	return atx, nil
}

func calcEligibleLayer(epochNumber types.EpochID, layersPerEpoch uint16, vrfHash [32]byte) types.LayerID {
	vrfInteger := binary.LittleEndian.Uint64(vrfHash[:8])
	eligibleLayerOffset := vrfInteger % uint64(layersPerEpoch)
	return epochNumber.FirstLayer().Add(uint16(eligibleLayerOffset))
}

func getNumberOfEligibleBlocks(activeSetSize, committeeSize uint32, layersPerEpoch uint16) (uint32, error) {
	if activeSetSize == 0 {
		return 0, errors.New("empty active set not allowed")
	}
	numberOfEligibleBlocks := committeeSize * uint32(layersPerEpoch) / activeSetSize
	if numberOfEligibleBlocks == 0 {
		numberOfEligibleBlocks = 1
	}
	return numberOfEligibleBlocks, nil
}

func (bo *Oracle) getATXIDForEpoch(targetEpoch types.EpochID) (types.ATXID, error) {
	latestATXID, err := bo.atxDB.GetNodeAtxIDForEpoch(bo.nodeID, targetEpoch)
	if err != nil {
		bo.log.With().Info("did not find atx ids for node",
			log.FieldNamed("atx_node_id", bo.nodeID),
			log.Err(err))
		return types.ATXID{}, err
	}
	bo.log.With().Info("latest atx id found", latestATXID)
	return latestATXID, err
}

func serializeVRFMessage(epochBeacon []byte, epochNumber types.EpochID, counter uint32) []byte {
	message := make([]byte, len(epochBeacon)+binary.Size(epochNumber)+binary.Size(counter))
	copy(message, epochBeacon)
	binary.LittleEndian.PutUint64(message[len(epochBeacon):], uint64(epochNumber))
	binary.LittleEndian.PutUint32(message[len(epochBeacon)+binary.Size(epochNumber):], counter)
	return message
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
