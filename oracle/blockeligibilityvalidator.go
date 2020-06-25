package oracle

import (
	"fmt"
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/sha256-simd"
)

// VRFValidationFunction is the VRF validation function.
type VRFValidationFunction func(message, signature, publicKey []byte) (bool, error)

// BlockEligibilityValidator holds all the dependencies for validating block eligibility.
type BlockEligibilityValidator struct {
	committeeSize      uint32
	genesisTotalWeight uint64
	layersPerEpoch     uint16
	activationDb       activationDB
	beaconProvider     *EpochBeaconProvider
	validateVRF        VRFValidationFunction
	log                log.Log
}

// NewBlockEligibilityValidator returns a new BlockEligibilityValidator.
func NewBlockEligibilityValidator(
	committeeSize uint32, genesisTotalWeight uint64, layersPerEpoch uint16, activationDb activationDB,
	beaconProvider *EpochBeaconProvider, validateVRF VRFValidationFunction, log log.Log) *BlockEligibilityValidator {

	return &BlockEligibilityValidator{
		committeeSize:      committeeSize,
		genesisTotalWeight: genesisTotalWeight,
		layersPerEpoch:     layersPerEpoch,
		activationDb:       activationDb,
		beaconProvider:     beaconProvider,
		validateVRF:        validateVRF,
		log:                log,
	}
}

// BlockSignedAndEligible checks that a given block is signed and eligible. It returns true with no error or false and
// an error that explains why validation failed.
func (v BlockEligibilityValidator) BlockSignedAndEligible(block *types.Block) (bool, error) {
	var weight, totalWeight uint64
	var vrfPubkey []byte
	var genesisNoAtx bool

	epochNumber := block.LayerIndex.GetEpoch(v.layersPerEpoch)
	if epochNumber == 0 {
		v.log.With().Warning("skipping epoch 0 block validation.",
			block.ID(), block.LayerIndex)
		return true, nil
	}
	if block.ATXID == *types.EmptyATXID {
		if !epochNumber.IsGenesis() {
			return false, fmt.Errorf("no associated ATX in epoch %v", epochNumber)
		}
		genesisNoAtx = true
	} else {
		atx, err := v.getValidAtx(block)
		if err != nil {
			return false, err
		}
		weight, totalWeight, vrfPubkey = atx.GetWeight(), atx.TotalWeight, atx.NodeID.VRFPublicKey
	}
	if epochNumber.IsGenesis() {
		v.log.With().Info("using genesisTotalWeight",
			block.ID(), log.Uint64("genesisTotalWeight", v.genesisTotalWeight))
		weight, totalWeight = 131072, v.genesisTotalWeight // TODO: replace 131072 with configured weight
	}

	numberOfEligibleBlocks, err := getNumberOfEligibleBlocks(weight, totalWeight, v.committeeSize, v.layersPerEpoch)
	if err != nil {
		return false, fmt.Errorf("failed to get number of eligible blocks: %v", err)
	}

	counter := block.EligibilityProof.J
	if counter >= numberOfEligibleBlocks {
		return false, fmt.Errorf("proof counter (%d) must be less than number of eligible blocks (%d)", counter,
			numberOfEligibleBlocks)
	}

	epochBeacon := v.beaconProvider.GetBeacon(epochNumber)
	message := serializeVRFMessage(epochBeacon, epochNumber, counter)
	vrfSig := block.EligibilityProof.Sig

	if genesisNoAtx {
		v.log.Info("skipping VRF validation, genesis block with no ATX") // TODO
		return true, nil
	}
	res, err := v.validateVRF(message, vrfSig, vrfPubkey)
	if err != nil {
		return false, fmt.Errorf("eligibility VRF validation failed: %v", err)
	}

	if !res {
		return false, fmt.Errorf("eligibility VRF validation failed")
	}

	eligibleLayer := calcEligibleLayer(epochNumber, v.layersPerEpoch, sha256.Sum256(vrfSig))

	if block.LayerIndex != eligibleLayer {
		return false, fmt.Errorf("block layer (%v) does not match eligibility layer (%v)",
			block.LayerIndex, eligibleLayer)
	}
	return true, nil
}

func (v BlockEligibilityValidator) getValidAtx(block *types.Block) (*types.ActivationTxHeader, error) {
	blockEpoch := block.LayerIndex.GetEpoch(v.layersPerEpoch)
	atx, err := v.activationDb.GetAtxHeader(block.ATXID)
	if err != nil {
		return nil, fmt.Errorf("getting ATX failed: %v %v ep(%v)", err, block.ATXID.ShortString(), blockEpoch)
	}
	if atxTargetEpoch := atx.PubLayerID.GetEpoch(v.layersPerEpoch) + 1; atxTargetEpoch != blockEpoch {
		return nil, fmt.Errorf("ATX target epoch (%d) doesn't match block publication epoch (%d)",
			atxTargetEpoch, blockEpoch)
	}
	if pubString := block.MinerID().String(); atx.NodeID.Key != pubString {
		return nil, fmt.Errorf("block signer (%s) mismatch with ATX node (%s)", pubString, atx.NodeID.Key)
	}
	return atx, nil
}
