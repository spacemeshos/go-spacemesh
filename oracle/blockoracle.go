package oracle

import (
	"encoding/binary"
	"errors"
	"fmt"
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/sha256-simd"
	"sync"
)

type ActivationDb interface {
	GetNodeAtxIDForEpoch(nodeId types.NodeId, targetEpoch types.EpochId) (types.AtxId, error)
	GetAtxHeader(id types.AtxId) (*types.ActivationTxHeader, error)
	GetIdentity(edId string) (types.NodeId, error)
}

type Signer interface {
	Sign(msg []byte) ([]byte, error)
}

type MinerBlockOracle struct {
	committeeSize        uint32
	genesisActiveSetSize uint32
	layersPerEpoch       uint16
	activationDb         ActivationDb
	beaconProvider       *EpochBeaconProvider
	vrfSigner            Signer
	nodeID               types.NodeId

	proofsEpoch       types.EpochId
	eligibilityProofs map[types.LayerID][]types.BlockEligibilityProof
	atxID             types.AtxId
	isSynced          func() bool
	eligibilityMutex  sync.RWMutex
	log               log.Log
}

func NewMinerBlockOracle(committeeSize uint32, genesisActiveSetSize uint32, layersPerEpoch uint16, activationDb ActivationDb, beaconProvider *EpochBeaconProvider, vrfSigner Signer, nodeId types.NodeId, isSynced func() bool, log log.Log) *MinerBlockOracle {

	return &MinerBlockOracle{
		committeeSize:        committeeSize,
		genesisActiveSetSize: genesisActiveSetSize,
		layersPerEpoch:       layersPerEpoch,
		activationDb:         activationDb,
		beaconProvider:       beaconProvider,
		vrfSigner:            vrfSigner,
		nodeID:               nodeId,
		proofsEpoch:          ^types.EpochId(0),
		isSynced:             isSynced,
		log:                  log,
	}
}

func (bo *MinerBlockOracle) BlockEligible(layerID types.LayerID) (types.AtxId, []types.BlockEligibilityProof, error) {
	if !bo.isSynced() {
		return types.AtxId{}, nil, fmt.Errorf("cannot calc eligibility, not synced yet")
	}
	epochNumber := layerID.GetEpoch(bo.layersPerEpoch)
	bo.log.Info("asked for eligibility for epoch %d (cached: %d)", epochNumber, bo.proofsEpoch)
	if bo.proofsEpoch != epochNumber {
		err := bo.calcEligibilityProofs(epochNumber)
		if err != nil {
			bo.log.Error("failed to calculate eligibility proofs: %v", err)
			return *types.EmptyAtxId, nil, err
		}
	}
	bo.eligibilityMutex.RLock()
	proofs := bo.eligibilityProofs[layerID]
	bo.eligibilityMutex.RUnlock()
	bo.log.Info("miner %v found eligible for %d blocks in layer %d", bo.nodeID.Key[:5], len(proofs), layerID)
	return bo.atxID, proofs, nil
}

func (bo *MinerBlockOracle) calcEligibilityProofs(epochNumber types.EpochId) error {
	bo.log.Info("calculating eligibility")
	epochBeacon := bo.beaconProvider.GetBeacon(epochNumber)

	var activeSetSize uint32
	atx, err := bo.getValidAtxForEpoch(epochNumber)
	if err != nil {
		if !epochNumber.IsGenesis() {
			return fmt.Errorf("failed to get latest ATX: %v", err)
		}
	} else {
		activeSetSize = atx.ActiveSetSize
		bo.atxID = atx.Id()
	}

	if epochNumber.IsGenesis() {
		activeSetSize = bo.genesisActiveSetSize
		bo.log.Info("genesis epoch detected, using GenesisActiveSetSize (%v)", activeSetSize)
	}

	numberOfEligibleBlocks, err := getNumberOfEligibleBlocks(activeSetSize, bo.committeeSize, bo.layersPerEpoch)
	if err != nil {
		bo.log.Error("failed to get number of eligible blocks: %v", err)
		return err
	}

	bo.eligibilityMutex.Lock()
	bo.eligibilityProofs = map[types.LayerID][]types.BlockEligibilityProof{}
	bo.eligibilityMutex.Unlock()
	for counter := uint32(0); counter < numberOfEligibleBlocks; counter++ {
		message := serializeVRFMessage(epochBeacon, epochNumber, counter)
		vrfSig, err := bo.vrfSigner.Sign(message)
		if err != nil {
			bo.log.Error("Could not sign message err=%v", err)
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
	bo.proofsEpoch = epochNumber
	bo.eligibilityMutex.RLock()
	bo.log.Info("miner %v is eligible for %d blocks on %d layers in epoch %d",
		bo.nodeID.Key[:5], numberOfEligibleBlocks, len(bo.eligibilityProofs), epochNumber)
	bo.eligibilityMutex.RUnlock()
	return nil
}

func (bo *MinerBlockOracle) getValidAtxForEpoch(validForEpoch types.EpochId) (*types.ActivationTxHeader, error) {
	atxId, err := bo.getAtxIdForEpoch(validForEpoch)
	if err != nil {
		return nil, fmt.Errorf("failed to get ATX ID for target epoch %v: %v", validForEpoch, err)
	}
	atx, err := bo.activationDb.GetAtxHeader(atxId)
	if err != nil {
		bo.log.Error("getting ATX failed: %v", err)
		return nil, err
	}
	return atx, nil
}

func calcEligibleLayer(epochNumber types.EpochId, layersPerEpoch uint16, vrfHash [32]byte) types.LayerID {
	vrfInteger := binary.LittleEndian.Uint64(vrfHash[:8])
	eligibleLayerOffset := vrfInteger % uint64(layersPerEpoch)
	return epochNumber.FirstLayer(layersPerEpoch).Add(uint16(eligibleLayerOffset))
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

func (bo *MinerBlockOracle) getAtxIdForEpoch(targetEpoch types.EpochId) (types.AtxId, error) {
	latestATXID, err := bo.activationDb.GetNodeAtxIDForEpoch(bo.nodeID, targetEpoch)
	if err != nil {
		bo.log.With().Info("did not find ATX IDs for node", log.String("atx_node_id", bo.nodeID.ShortString()), log.Err(err))
		return types.AtxId{}, err
	}
	bo.log.With().Info("latest atx id found", log.AtxID(latestATXID.ShortString()))
	return latestATXID, err
}

func serializeVRFMessage(epochBeacon []byte, epochNumber types.EpochId, counter uint32) []byte {
	message := make([]byte, len(epochBeacon)+binary.Size(epochNumber)+binary.Size(counter))
	copy(message, epochBeacon)
	binary.LittleEndian.PutUint64(message[len(epochBeacon):], uint64(epochNumber))
	binary.LittleEndian.PutUint32(message[len(epochBeacon)+binary.Size(epochNumber):], counter)
	return message
}

func (bo *MinerBlockOracle) GetEligibleLayers() []types.LayerID {
	bo.eligibilityMutex.RLock()
	layers := make([]types.LayerID, 0, len(bo.eligibilityProofs))
	for layer := range bo.eligibilityProofs {
		layers = append(layers, layer)
	}
	bo.eligibilityMutex.RUnlock()
	return layers
}
