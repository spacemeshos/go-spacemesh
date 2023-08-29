package proposals

import (
	"context"
	"errors"
	"fmt"

	"github.com/spacemeshos/fixed"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/datastore"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/sql/ballots"
	"github.com/spacemeshos/go-spacemesh/system"
)

var (
	errTargetEpochMismatch = errors.New("ATX target epoch and ballot publish epoch mismatch")
	errPublicKeyMismatch   = errors.New("ballot smesher key and ATX node key mismatch")
	errIncorrectCounter    = errors.New("proof counter larger than number of slots available")
	errInvalidProofsOrder  = errors.New("proofs are out of order")
	errIncorrectVRFSig     = errors.New("proof contains incorrect VRF signature")
	errIncorrectLayerIndex = errors.New("ballot has incorrect layer index")
	errIncorrectEligCount  = errors.New("ballot has incorrect eligibility count")
)

// Validator validates the eligibility of a Ballot.
// the validation focuses on eligibility only and assumes the Ballot to be valid otherwise.
type Validator struct {
	minActiveSetWeight uint64
	avgLayerSize       uint32
	layersPerEpoch     uint32
	cdb                *datastore.CachedDB
	beacons            system.BeaconCollector
	logger             log.Log
	vrfVerifier        vrfVerifier
	nonceFetcher       nonceFetcher
}

// ValidatorOpt for configuring Validator.
type ValidatorOpt func(h *Validator)

func WithNonceFetcher(nf nonceFetcher) ValidatorOpt {
	return func(h *Validator) {
		h.nonceFetcher = nf
	}
}

// NewEligibilityValidator returns a new EligibilityValidator.
func NewEligibilityValidator(
	avgLayerSize, layersPerEpoch uint32, minActiveSetWeight uint64, cdb *datastore.CachedDB, bc system.BeaconCollector, lg log.Log, vrfVerifier vrfVerifier, opts ...ValidatorOpt,
) *Validator {
	v := &Validator{
		minActiveSetWeight: minActiveSetWeight,
		avgLayerSize:       avgLayerSize,
		layersPerEpoch:     layersPerEpoch,
		cdb:                cdb,
		beacons:            bc,
		logger:             lg,
		vrfVerifier:        vrfVerifier,
	}
	for _, opt := range opts {
		opt(v)
	}
	if v.nonceFetcher == nil {
		v.nonceFetcher = cdb
	}
	return v
}

// CheckEligibility checks that a ballot is eligible in the layer that it specifies.
func (v *Validator) CheckEligibility(ctx context.Context, ballot *types.Ballot) (bool, error) {
	if len(ballot.EligibilityProofs) == 0 {
		return false, fmt.Errorf("empty eligibility list is invalid (ballot %s)", ballot.ID())
	}
	owned, err := v.cdb.GetAtxHeader(ballot.AtxID)
	if err != nil {
		return false, fmt.Errorf("ballot has no atx %v: %w", ballot.AtxID, err)
	}
	var (
		beacon           types.Beacon
		numEligibleSlots uint32
	)
	if ballot.EpochData != nil {
		beacon = ballot.EpochData.Beacon
		if beacon == types.EmptyBeacon {
			return false, fmt.Errorf("%w: ref ballot %v", errMissingBeacon, ballot.ID())
		}
		if len(ballot.ActiveSet) == 0 {
			return false, fmt.Errorf("%w: ref ballot %v", errEmptyActiveSet, ballot.ID())
		}
		var totalWeight uint64
		for _, atxID := range ballot.ActiveSet {
			atx, err := v.cdb.GetAtxHeader(atxID)
			if err != nil {
				return false, fmt.Errorf("get ATX header %v: %w", atxID, err)
			}
			totalWeight += atx.GetWeight()
		}
		if owned.TargetEpoch() != ballot.Layer.GetEpoch() {
			return false, fmt.Errorf("%w: ATX target epoch (%v), ballot publication epoch (%v)",
				errTargetEpochMismatch, owned.TargetEpoch(), ballot.Layer.GetEpoch())
		}
		if ballot.SmesherID != owned.NodeID {
			return false, fmt.Errorf("%w: public key (%v), ATX node key (%v)", errPublicKeyMismatch, ballot.SmesherID.String(), owned.NodeID)
		}
		numEligibleSlots, err = GetLegacyNumEligible(ballot.Layer, owned.GetWeight(), v.minActiveSetWeight, totalWeight, v.avgLayerSize, v.layersPerEpoch)
		if err != nil {
			return false, err
		}
		if ballot.EpochData.EligibilityCount != numEligibleSlots {
			return false, fmt.Errorf("%w: expected %v, got: %v", errIncorrectEligCount, numEligibleSlots, ballot.EpochData.EligibilityCount)
		}
	} else {
		if ballot.RefBallot == types.EmptyBallotID {
			return false, fmt.Errorf("empty ballot as ref ballot for %v", ballot.RefBallot)
		}
		refBallot, err := ballots.Get(v.cdb, ballot.RefBallot)
		if err != nil {
			return false, fmt.Errorf("get ref ballot %v: %w", ballot.RefBallot, err)
		}
		if refBallot.EpochData == nil {
			return false, fmt.Errorf("%w: ref ballot %v", errMissingEpochData, refBallot.ID())
		}
		if refBallot.AtxID != ballot.AtxID {
			return false, fmt.Errorf("ballot (%v/%v) should be sharing atx with a reference ballot (%v/%v)", ballot.ID(), ballot.AtxID, refBallot.ID(), refBallot.AtxID)
		}
		if refBallot.SmesherID != ballot.SmesherID {
			return false, fmt.Errorf("mismatched smesher id with refballot in ballot %v", ballot.ID())
		}
		if refBallot.Layer.GetEpoch() != ballot.Layer.GetEpoch() {
			return false, fmt.Errorf("ballot %v targets mismatched epoch %d", ballot.ID(), ballot.Layer.GetEpoch())
		}
		numEligibleSlots = refBallot.EpochData.EligibilityCount
		beacon = refBallot.EpochData.Beacon
	}

	nonce, err := v.nonceFetcher.VRFNonce(ballot.SmesherID, ballot.Layer.GetEpoch())
	if err != nil {
		return false, err
	}
	var (
		last    uint32
		isFirst = true
	)
	for _, proof := range ballot.EligibilityProofs {
		counter := proof.J
		if counter >= numEligibleSlots {
			return false, fmt.Errorf("%w: proof counter (%d) numEligibleBallots (%d)",
				errIncorrectCounter, counter, numEligibleSlots)
		}
		if isFirst {
			isFirst = false
		} else if counter <= last {
			return false, fmt.Errorf("%w: %d <= %d", errInvalidProofsOrder, counter, last)
		}
		last = counter

		message, err := SerializeVRFMessage(beacon, ballot.Layer.GetEpoch(), nonce, counter)
		if err != nil {
			return false, err
		}

		if !v.vrfVerifier.Verify(ballot.SmesherID, message, proof.Sig) {
			return false, fmt.Errorf("%w: beacon: %v, epoch: %v, counter: %v, vrfSig: %s",
				errIncorrectVRFSig, beacon.ShortString(), ballot.Layer.GetEpoch(), counter, proof.Sig,
			)
		}

		eligibleLayer := CalcEligibleLayer(ballot.Layer.GetEpoch(), v.layersPerEpoch, proof.Sig)
		if ballot.Layer != eligibleLayer {
			return false, fmt.Errorf("%w: ballot layer (%v), eligible layer (%v)",
				errIncorrectLayerIndex, ballot.Layer, eligibleLayer)
		}
	}

	v.logger.WithContext(ctx).With().Debug("ballot eligibility verified",
		ballot.ID(),
		ballot.Layer,
		ballot.Layer.GetEpoch(),
		beacon,
	)

	weightPer := fixed.DivUint64(owned.GetWeight(), uint64(numEligibleSlots))
	v.beacons.ReportBeaconFromBallot(ballot.Layer.GetEpoch(), ballot, beacon, weightPer)
	return true, nil
}
