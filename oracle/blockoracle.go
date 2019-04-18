package oracle

import (
	"encoding/binary"
	"errors"
	"fmt"
	"github.com/spacemeshos/go-spacemesh/crypto"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/types"
	"github.com/spacemeshos/sha256-simd"
)

const GenesisActiveSetSize = 10

type ActivationDb interface {
	GetNodeAtxIds(node types.NodeId) ([]types.AtxId, error)
	GetAtx(id types.AtxId) (*types.ActivationTx, error)
}

type MinerBlockOracle struct {
	committeeSize  int32
	layersPerEpoch uint16
	activationDb   ActivationDb
	beaconProvider *EpochBeaconProvider
	vrfSigner      *crypto.VRFSigner
	nodeID         types.NodeId

	proofsEpoch       types.EpochId
	eligibilityProofs map[types.LayerID][]types.BlockEligibilityProof
	atxID             types.AtxId
	log               log.Log
}

func NewMinerBlockOracle(committeeSize int32, layersPerEpoch uint16, activationDb ActivationDb,
	beaconProvider *EpochBeaconProvider, vrfSigner *crypto.VRFSigner, nodeId types.NodeId,
	log log.Log) *MinerBlockOracle {

	return &MinerBlockOracle{
		committeeSize:  committeeSize,
		layersPerEpoch: layersPerEpoch,
		activationDb:   activationDb,
		beaconProvider: beaconProvider,
		vrfSigner:      vrfSigner,
		nodeID:         nodeId,
		proofsEpoch:    ^types.EpochId(0),
		log:            log,
	}
}

func (bo *MinerBlockOracle) BlockEligible(layerID types.LayerID) (types.AtxId, []types.BlockEligibilityProof, error) {

	epochNumber := layerID.GetEpoch(bo.layersPerEpoch)
	bo.log.Info("asked for eligibility for epoch %d (cached: %d)", epochNumber, bo.proofsEpoch)
	if bo.proofsEpoch != epochNumber {
		err := bo.calcEligibilityProofs(epochNumber)
		if err != nil {
			bo.log.Error("failed to calculate eligibility proofs: %v", err)
			return types.AtxId{}, nil, err
		}
	}
	bo.log.Info("miner \"%v…\" found eligible for %d blocks in layer %d", bo.nodeID.Key[:5], len(bo.eligibilityProofs[layerID]), layerID)
	return bo.atxID, bo.eligibilityProofs[layerID], nil
}

func (bo *MinerBlockOracle) calcEligibilityProofs(epochNumber types.EpochId) error {
	epochBeacon := bo.beaconProvider.GetBeacon(epochNumber)

	activeSetSize, err := bo.calcActiveSetSize(epochNumber)
	if err != nil {
		bo.log.Error("failed to calculate active set size: %v", err)
		return fmt.Errorf("failed to calculate active set size: %v", err)
	}
	numberOfEligibleBlocks, err := getNumberOfEligibleBlocks(activeSetSize, bo.committeeSize, bo.layersPerEpoch, bo.log)
	if err != nil {
		bo.log.Error("failed to get number of eligible blocks: %v", err)
		return err
	}

	bo.eligibilityProofs = map[types.LayerID][]types.BlockEligibilityProof{}
	for counter := uint32(0); counter < numberOfEligibleBlocks; counter++ {
		message := serializeVRFMessage(epochBeacon, epochNumber, counter)
		vrfSig := bo.vrfSigner.Sign(message)
		vrfHash := sha256.Sum256(vrfSig)
		eligibleLayer := calcEligibleLayer(epochNumber, bo.layersPerEpoch, vrfHash)
		bo.eligibilityProofs[eligibleLayer] = append(bo.eligibilityProofs[eligibleLayer], types.BlockEligibilityProof{
			J:   counter,
			Sig: vrfSig,
		})
	}
	bo.proofsEpoch = epochNumber
	bo.log.Info("miner \"%v…\" is eligible for blocks on %d layers in epoch %d", bo.nodeID.Key[:5], len(bo.eligibilityProofs), epochNumber)
	return nil
}

func (bo *MinerBlockOracle) calcActiveSetSize(epochNumber types.EpochId) (uint32, error) {
	if epochNumber.IsGenesis() {
		bo.log.Warning("genesis epoch detected, using GenesisActiveSetSize (%d)", GenesisActiveSetSize)
		return GenesisActiveSetSize, nil
	}

	latestATXID, err := bo.getLatestATXID(bo.activationDb, bo.nodeID)
	if err != nil {
		bo.log.Error("failed to get latest ATX: %v for epoch %v", err, epochNumber)
		return 0, err
	}
	atx, err := bo.activationDb.GetAtx(latestATXID)
	if err != nil {
		bo.log.Error("getting ATX failed: %v", err)
		return 0, err
	}
	if epochNumber != atx.PubLayerIdx.GetEpoch(bo.layersPerEpoch)+1 {
		bo.log.Warning("latest ATX (epoch %d) not valid in current epoch (%d), miner not eligible",
			atx.PubLayerIdx.GetEpoch(bo.layersPerEpoch), epochNumber)
		return 0, fmt.Errorf("latest ATX not valid in current epoch")
	}
	bo.atxID = latestATXID
	activeSetSize := atx.ActiveSetSize
	return activeSetSize, nil
}

func calcEligibleLayer(epochNumber types.EpochId, layersPerEpoch uint16, vrfHash [32]byte) types.LayerID {
	epochOffset := uint64(epochNumber) * uint64(layersPerEpoch)
	vrfInteger := binary.LittleEndian.Uint64(vrfHash[:8])
	eligibleLayerRelativeToEpochStart := vrfInteger % uint64(layersPerEpoch)
	return types.LayerID(eligibleLayerRelativeToEpochStart + epochOffset)
}

func getNumberOfEligibleBlocks(activeSetSize uint32, committeeSize int32, layersPerEpoch uint16, lg log.Log) (uint32, error) {
	if activeSetSize == 0 {
		lg.Error("empty active set detected in activation transaction")
		return 0, errors.New("empty active set not allowed")
	}
	numberOfEligibleBlocks := uint32(committeeSize) * uint32(layersPerEpoch) / activeSetSize
	if numberOfEligibleBlocks == 0 {
		numberOfEligibleBlocks = 1
	}
	return numberOfEligibleBlocks, nil
}

func (bo *MinerBlockOracle) getLatestATXID(activationDb ActivationDb, nodeID types.NodeId) (types.AtxId, error) {
	atxIDs, err := activationDb.GetNodeAtxIds(nodeID)
	if err != nil {
		bo.log.Error("getting node ATX IDs failed: %v", err)
		return types.AtxId{}, err
	}
	numOfActivations := len(atxIDs)
	if numOfActivations < 1 {
		bo.log.Error("no activations found for node id \"%v\"", nodeID.Key)
		return types.AtxId{}, errors.New("no activations found")
	}
	latestATXID := atxIDs[numOfActivations-1]
	return latestATXID, err
}

func serializeVRFMessage(epochBeacon []byte, epochNumber types.EpochId, counter uint32) []byte {
	message := make([]byte, len(epochBeacon)+binary.Size(epochNumber)+binary.Size(counter))
	copy(message, epochBeacon)
	binary.LittleEndian.PutUint64(message[len(epochBeacon):], uint64(epochNumber))
	binary.LittleEndian.PutUint32(message[len(epochBeacon)+binary.Size(epochNumber):], counter)
	return message
}
