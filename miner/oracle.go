package miner

import (
	"bytes"
	"errors"
	"fmt"
	"sort"
	"sync"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/datastore"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/proposals"
	"github.com/spacemeshos/go-spacemesh/signing"
	"github.com/spacemeshos/go-spacemesh/sql"
	"github.com/spacemeshos/go-spacemesh/sql/atxs"
)

var (
	errMinerHasNoATXInPreviousEpoch = errors.New("miner has no ATX in previous epoch")
	errZeroEpochWeight              = errors.New("zero total weight for epoch")
	errEmptyActiveSet               = errors.New("empty active set for epoch")
)

type EpochEligibility struct {
	Epoch     types.EpochID
	Atx       types.ATXID
	ActiveSet types.ATXIDList
	Proofs    map[types.LayerID][]types.VotingEligibility
	Slots     uint32
}

// Oracle provides proposal eligibility proofs for the miner.
type Oracle struct {
	avgLayerSize   uint32
	layersPerEpoch uint32
	cdb            *datastore.CachedDB

	vrfSigner *signing.VRFSigner
	nodeID    types.NodeID
	log       log.Log

	mu    sync.Mutex
	cache *EpochEligibility
}

func newMinerOracle(layerSize, layersPerEpoch uint32, cdb *datastore.CachedDB, vrfSigner *signing.VRFSigner, nodeID types.NodeID, log log.Log) *Oracle {
	return &Oracle{
		avgLayerSize:   layerSize,
		layersPerEpoch: layersPerEpoch,
		cdb:            cdb,
		vrfSigner:      vrfSigner,
		nodeID:         nodeID,
		log:            log,
		cache:          &EpochEligibility{},
	}
}

// GetProposalEligibility returns the miner's ATXID and the active set for the layer's epoch, along with the list of eligibility
// proofs for that layer.
func (o *Oracle) GetProposalEligibility(lid types.LayerID, beacon types.Beacon, nonce types.VRFPostIndex) (*EpochEligibility, error) {
	o.mu.Lock()
	defer o.mu.Unlock()

	epoch := lid.GetEpoch()
	if lid <= types.GetEffectiveGenesis() {
		o.log.With().Panic("eligibility should not be queried during genesis", lid, epoch)
	}

	o.log.With().Debug("asked for proposal eligibility",
		log.Stringer("requested epoch", epoch),
		log.Stringer("cached epoch", o.cache.Epoch),
	)

	var layerProofs []types.VotingEligibility
	if o.cache.Epoch == epoch { // use the cached value
		layerProofs = o.cache.Proofs[lid]
		o.log.With().Debug("got cached eligibility",
			log.Stringer("requested epoch", epoch),
			log.Stringer("cached epoch", o.cache.Epoch),
			log.Int("num proposals", len(layerProofs)),
		)
		return o.cache, nil
	}

	// calculate the proof
	atx, err := o.getOwnEpochATX(epoch)
	if err != nil {
		if errors.Is(err, sql.ErrNotFound) {
			return nil, errMinerHasNoATXInPreviousEpoch
		}
		return nil, fmt.Errorf("failed to get valid atx for node for target epoch %d: %w", epoch, err)
	}

	ee, err := o.calcEligibilityProofs(atx, epoch, beacon, nonce)
	if err != nil {
		return nil, err
	}
	o.cache = ee
	return ee, nil
}

func (o *Oracle) getOwnEpochATX(targetEpoch types.EpochID) (*types.ActivationTxHeader, error) {
	publishEpoch := targetEpoch - 1
	atxID, err := atxs.GetIDByEpochAndNodeID(o.cdb, publishEpoch, o.nodeID)
	if err != nil {
		return nil, fmt.Errorf("get ATX ID: %w", err)
	}

	atx, err := o.cdb.GetAtxHeader(atxID)
	if err != nil {
		return nil, fmt.Errorf("get ATX header: %w", err)
	}
	return atx, nil
}

// calcEligibilityProofs calculates the eligibility proofs of proposals for the miner in the given epoch
// and returns the proofs along with the epoch's active set.
func (o *Oracle) calcEligibilityProofs(atx *types.ActivationTxHeader, epoch types.EpochID, beacon types.Beacon, nonce types.VRFPostIndex) (*EpochEligibility, error) {
	weight := atx.GetWeight()

	// get the previous epoch's total weight
	totalWeight, activeSet, err := o.cdb.GetEpochWeight(epoch)
	if err != nil {
		return nil, fmt.Errorf("failed to get epoch %v weight: %w", epoch, err)
	}
	if totalWeight == 0 {
		return nil, errZeroEpochWeight
	}
	if len(activeSet) == 0 {
		return nil, errEmptyActiveSet
	}

	o.log.With().Debug("calculating eligibility",
		epoch,
		beacon,
		log.Uint64("weight", weight),
		log.Uint64("total weight", totalWeight),
	)

	numEligibleSlots, err := proposals.GetNumEligibleSlots(weight, totalWeight, o.avgLayerSize, o.layersPerEpoch)
	if err != nil {
		return nil, fmt.Errorf("oracle get num slots: %w", err)
	}

	eligibilityProofs := map[types.LayerID][]types.VotingEligibility{}
	for counter := uint32(0); counter < numEligibleSlots; counter++ {
		message, err := proposals.SerializeVRFMessage(beacon, epoch, nonce, counter)
		if err != nil {
			o.log.With().Fatal("failed to serialize VRF msg", log.Err(err))
		}
		vrfSig := o.vrfSigner.Sign(message)
		eligibleLayer := proposals.CalcEligibleLayer(epoch, o.layersPerEpoch, vrfSig)
		eligibilityProofs[eligibleLayer] = append(eligibilityProofs[eligibleLayer], types.VotingEligibility{
			J:   counter,
			Sig: vrfSig,
		})
		o.log.Debug(fmt.Sprintf("signed vrf message, counter: %v, vrfSig: %s, layer: %v", counter, vrfSig, eligibleLayer))
	}

	o.log.With().Info("proposal eligibility for an epoch",
		epoch,
		beacon,
		log.Uint64("weight", weight),
		log.Uint64("total weight", totalWeight),
		log.Uint32("total num slots", numEligibleSlots),
		log.Int("num layers eligible", len(eligibilityProofs)),
		log.Array("layers to num proposals", log.ArrayMarshalerFunc(func(encoder log.ArrayEncoder) error {
			// Sort the layer map to log the layer data in order
			keys := make([]types.LayerID, 0, len(eligibilityProofs))
			for k := range eligibilityProofs {
				keys = append(keys, k)
			}
			sort.Slice(keys, func(i, j int) bool {
				return keys[i].Before(keys[j])
			})
			for _, lyr := range keys {
				encoder.AppendObject(log.ObjectMarshallerFunc(func(encoder log.ObjectEncoder) error {
					encoder.AddUint32("layer", lyr.Uint32())
					encoder.AddInt("slots", len(eligibilityProofs[lyr]))
					return nil
				}))
			}
			return nil
		})))
	sort.Slice(activeSet, func(i, j int) bool {
		return bytes.Compare(activeSet[i].Bytes(), activeSet[j].Bytes()) < 0
	})
	return &EpochEligibility{
		Epoch:     epoch,
		Atx:       atx.ID,
		ActiveSet: activeSet,
		Proofs:    eligibilityProofs,
		Slots:     numEligibleSlots,
	}, nil
}
