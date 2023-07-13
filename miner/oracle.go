package miner

import (
	"bytes"
	"errors"
	"fmt"
	"sort"
	"sync"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/datastore"
	"github.com/spacemeshos/go-spacemesh/events"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/proposals"
	"github.com/spacemeshos/go-spacemesh/signing"
	"github.com/spacemeshos/go-spacemesh/system"
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
	cfg       config
	clock     layerClock
	cdb       *datastore.CachedDB
	vrfSigner *signing.VRFSigner
	syncState system.SyncStateProvider
	log       log.Log

	mu    sync.Mutex
	cache *EpochEligibility
}

func newMinerOracle(cfg config, c layerClock, cdb *datastore.CachedDB, vrfSigner *signing.VRFSigner, ss system.SyncStateProvider, log log.Log) *Oracle {
	return &Oracle{
		cfg:       cfg,
		clock:     c,
		cdb:       cdb,
		vrfSigner: vrfSigner,
		syncState: ss,
		log:       log,
		cache:     &EpochEligibility{},
	}
}

// ProposalEligibility returns the miner's ATXID and the active set for the layer's epoch, along with the list of eligibility
// proofs for that layer.
func (o *Oracle) ProposalEligibility(lid types.LayerID, beacon types.Beacon, nonce types.VRFPostIndex) (*EpochEligibility, error) {
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

	ee, err := o.calcEligibilityProofs(epoch, beacon, nonce)
	if err != nil {
		return nil, err
	}
	events.EmitEligibilities(ee.Epoch, beacon, ee.Atx, uint32(len(ee.ActiveSet)), ee.Proofs)
	o.cache = ee
	return ee, nil
}

func (o *Oracle) activesFromFirstBlock(targetEpoch types.EpochID) (uint64, uint64, []types.ATXID, error) {
	activeSet, err := ActiveSetFromBlock(o.cdb, targetEpoch)
	if err != nil {
		return 0, 0, nil, err
	}
	own, err := o.cdb.GetEpochAtx(targetEpoch-1, o.cfg.nodeID)
	if err != nil {
		return 0, 0, nil, err
	}
	// put miner's own ATXID last
	activeSet = append(activeSet, own.ID)
	var total uint64
	for _, id := range activeSet {
		hdr, err := o.cdb.GetAtxHeader(id)
		if err != nil {
			return 0, 0, nil, err
		}
		total += hdr.GetWeight()
	}
	o.log.With().Info("active set selected for proposal",
		log.Stringer("epoch", targetEpoch),
		log.Int("num_atx", len(activeSet)),
	)
	return own.GetWeight(), total, activeSet, nil
}

func (o *Oracle) activeSet(targetEpoch types.EpochID) (uint64, uint64, []types.ATXID, error) {
	if !o.syncState.SyncedBefore(targetEpoch - 1) {
		// if the node is not synced prior to `targetEpoch-1`, it doesn't have the correct receipt timestamp
		// for all the atx and malfeasance proof, and cannot use atx grading for active set.
		o.log.With().Info("node not synced before prior epoch, getting active set from first block",
			log.Stringer("epoch", targetEpoch),
		)
		return o.activesFromFirstBlock(targetEpoch)
	}

	var (
		minerWeight, totalWeight uint64
		minerID                  types.ATXID
		atxids                   []types.ATXID
	)

	epochStart := o.clock.LayerToTime(targetEpoch.FirstLayer())
	numOmitted := 0
	if err := o.cdb.IterateEpochATXHeaders(targetEpoch, func(header *types.ActivationTxHeader) error {
		grade, err := GradeAtx(o.cdb, header.NodeID, header.Received, epochStart, o.cfg.networkDelay)
		if err != nil {
			return err
		}
		if grade != Good {
			o.log.With().Info("atx omitted from active set",
				header.ID,
				log.Int("grade", int(grade)),
				log.Stringer("smesher", header.NodeID),
				log.Time("received", header.Received),
				log.Time("epoch_start", epochStart),
			)
			numOmitted++
			return nil
		}
		totalWeight += header.GetWeight()
		if header.NodeID == o.cfg.nodeID {
			minerWeight = header.GetWeight()
			minerID = header.ID
		} else {
			atxids = append(atxids, header.ID)
		}
		return nil
	}); err != nil {
		return 0, 0, nil, err
	}
	// put miner's own ATXID last
	if minerID != types.EmptyATXID {
		atxids = append(atxids, minerID)
	}
	o.log.With().Info("active set selected for proposal",
		log.Int("num_atx", len(atxids)),
		log.Int("num_omitted", numOmitted),
	)
	return minerWeight, totalWeight, atxids, nil
}

// calcEligibilityProofs calculates the eligibility proofs of proposals for the miner in the given epoch
// and returns the proofs along with the epoch's active set.
func (o *Oracle) calcEligibilityProofs(epoch types.EpochID, beacon types.Beacon, nonce types.VRFPostIndex) (*EpochEligibility, error) {
	// get the previous epoch's total weight
	minerWeight, totalWeight, activeSet, err := o.activeSet(epoch)
	if err != nil {
		return nil, fmt.Errorf("failed to get epoch %v weight: %w", epoch, err)
	}
	if minerWeight == 0 {
		return nil, errMinerHasNoATXInPreviousEpoch
	}
	if totalWeight == 0 {
		return nil, errZeroEpochWeight
	}
	if len(activeSet) == 0 {
		return nil, errEmptyActiveSet
	}
	ownAtx := activeSet[len(activeSet)-1]
	o.log.With().Debug("calculating eligibility",
		epoch,
		beacon,
		log.Uint64("weight", minerWeight),
		log.Uint64("total weight", totalWeight),
	)

	numEligibleSlots, err := proposals.GetNumEligibleSlots(minerWeight, o.cfg.minActiveSetWeight, totalWeight, o.cfg.layerSize, o.cfg.layersPerEpoch)
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
		eligibleLayer := proposals.CalcEligibleLayer(epoch, o.cfg.layersPerEpoch, vrfSig)
		eligibilityProofs[eligibleLayer] = append(eligibilityProofs[eligibleLayer], types.VotingEligibility{
			J:   counter,
			Sig: vrfSig,
		})
		o.log.Debug(fmt.Sprintf("signed vrf message, counter: %v, vrfSig: %s, layer: %v", counter, vrfSig, eligibleLayer))
	}

	o.log.With().Info("proposal eligibility for an epoch",
		epoch,
		beacon,
		log.Uint64("weight", minerWeight),
		log.Uint64("min activeset weight", o.cfg.minActiveSetWeight),
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
		Atx:       ownAtx,
		ActiveSet: activeSet,
		Proofs:    eligibilityProofs,
		Slots:     numEligibleSlots,
	}, nil
}
