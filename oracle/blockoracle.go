package oracle

import (
	"crypto/sha256"
	"encoding/binary"
	"github.com/spacemeshos/go-spacemesh/crypto"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/mesh"
)

type ActivationDb interface {
	GetNodeAtxIds(node mesh.NodeId) ([]mesh.AtxId, error)
	GetAtx(id mesh.AtxId) (*mesh.ActivationTx, error)
}

type MinerBlockOracle struct {
	committeeSize  int32
	layersPerEpoch uint16
	activationDb   ActivationDb
	beaconProvider *EpochBeaconProvider
	vrfSigner      *crypto.VRFSigner
	nodeID         mesh.NodeId

	eligibilityProofs map[mesh.LayerID][]mesh.BlockEligibilityProof
	proofsEpoch       uint64
}

func NewMinerBlockOracle(committeeSize int32, layersPerEpoch uint16, activationDb ActivationDb,
	beaconProvider *EpochBeaconProvider, vrfSigner *crypto.VRFSigner, nodeId mesh.NodeId) *MinerBlockOracle {

	return &MinerBlockOracle{
		committeeSize:  committeeSize,
		layersPerEpoch: layersPerEpoch,
		activationDb:   activationDb,
		beaconProvider: beaconProvider,
		vrfSigner:      vrfSigner,
		nodeID:         nodeId,
		proofsEpoch:    ^uint64(0),
	}
}

func (bo *MinerBlockOracle) BlockEligible(layerID mesh.LayerID) ([]mesh.BlockEligibilityProof, error) {

	epochNumber := uint64(layerID) / uint64(bo.layersPerEpoch)

	if bo.proofsEpoch != epochNumber {
		err := bo.calcEligibilityProofs(epochNumber)
		if err != nil {
			return nil, err
		}
	}

	return bo.eligibilityProofs[layerID], nil
}

func (bo *MinerBlockOracle) calcEligibilityProofs(epochNumber uint64) error {
	epochBeacon := bo.beaconProvider.GetBeacon(epochNumber)

	atxIDs, err := bo.activationDb.GetNodeAtxIds(bo.nodeID)
	if err != nil {
		log.Error("getting previous atx id failed: %v", err)
		return err
	}
	atxId := atxIDs[len(atxIDs)-1]
	atx, err := bo.activationDb.GetAtx(atxId)
	activeSetSize := atx.ActiveSetSize

	numberOfEligibleBlocks := uint32(bo.committeeSize) * uint32(bo.layersPerEpoch) / activeSetSize
	if numberOfEligibleBlocks == 0 {
		numberOfEligibleBlocks = 1
	}

	bo.eligibilityProofs = map[mesh.LayerID][]mesh.BlockEligibilityProof{}
	for counter := uint32(0); counter < numberOfEligibleBlocks; counter++ {
		message := make([]byte, len(epochBeacon)+binary.Size(epochNumber)+binary.Size(counter))
		copy(message, epochBeacon)
		binary.LittleEndian.PutUint64(message[len(epochBeacon):], epochNumber)
		binary.LittleEndian.PutUint32(message[len(epochBeacon)+binary.Size(epochNumber):], counter)
		sig := bo.vrfSigner.Sign(message)
		hash := sha256.Sum256(sig)
		eligibleLayer := mesh.LayerID(binary.LittleEndian.Uint64(hash[:]) % uint64(bo.layersPerEpoch))
		bo.eligibilityProofs[eligibleLayer] = append(bo.eligibilityProofs[eligibleLayer], mesh.BlockEligibilityProof{
			J:   counter,
			Sig: sig,
		})
	}
	return nil
}
