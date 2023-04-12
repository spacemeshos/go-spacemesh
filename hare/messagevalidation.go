package hare

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/signing"
)

type messageValidator interface {
	SyntacticallyValidateMessage(ctx context.Context, m *Msg) bool
	ContextuallyValidateMessage(ctx context.Context, m *Msg, expectedK uint32) error
}

type eligibilityValidator struct {
	oracle        Rolacle
	maxExpActives int // the maximal expected committee size
	expLeaders    int // the expected number of leaders
	log.Log
}

func newEligibilityValidator(oracle Rolacle, maxExpActives, expLeaders int, logger log.Log) *eligibilityValidator {
	return &eligibilityValidator{oracle, maxExpActives, expLeaders, logger}
}

func (ev *eligibilityValidator) validateRole(ctx context.Context, nodeID types.NodeID, layer types.LayerID, round uint32, proof types.VrfSignature, eligibilityCount uint16) (bool, error) {
	return ev.oracle.Validate(ctx, layer, round, expectedCommitteeSize(round, ev.maxExpActives, ev.expLeaders), nodeID, proof, eligibilityCount)
}

func (ev *eligibilityValidator) ValidateEligibilityGossip(ctx context.Context, em *types.HareEligibilityGossip) bool {
	res, err := ev.validateRole(ctx, em.NodeID, em.Layer, em.Round, em.Eligibility.Proof, em.Eligibility.Count)
	if err != nil {
		ev.WithContext(ctx).With().Error("failed to validate role",
			em.Layer,
			log.Uint32("round", em.Round),
			log.Stringer("smesher", em.NodeID),
		)
		return false
	}
	return res
}

// Validate the eligibility of the provided message.
func (ev *eligibilityValidator) Validate(ctx context.Context, m *Msg) bool {
	if m == nil || m.InnerMessage == nil {
		ev.Log.Fatal("invalid Msg")
	}

	res, err := ev.validateRole(ctx, m.NodeID, m.Layer, m.Round, m.Eligibility.Proof, m.Eligibility.Count)
	if err != nil {
		ev.WithContext(ctx).With().Error("failed to validate role",
			log.Err(err),
			log.Stringer("smesher", m.NodeID),
			m.Layer,
			log.String("msg_type", m.Type.String()),
		)
		return false
	}

	if !res {
		ev.WithContext(ctx).With().Warning("validate message failed: role is invalid",
			log.Stringer("smesher", m.NodeID),
			m.Layer,
			log.String("msg_type", m.Type.String()),
		)
		return false
	}

	return true
}

type roleValidator interface {
	Validate(context.Context, *Msg) bool
}

type pubKeyGetter interface {
	Track(*Msg)
	NodeID(*Message) types.NodeID
}

type syntaxContextValidator struct {
	signing          *signing.EdSigner
	pubKeyExtractor  *signing.PubKeyExtractor
	threshold        int
	statusValidator  func(m *Msg) bool // used to validate status Messages in SVP
	stateQuerier     stateQuerier
	roleValidator    roleValidator
	validMsgsTracker pubKeyGetter // used to check for public keys in the valid messages tracker
	eTracker         *EligibilityTracker
	log.Log
}

func newSyntaxContextValidator(
	sgr *signing.EdSigner,
	pubKeyExtractor *signing.PubKeyExtractor,
	threshold int,
	validator func(m *Msg) bool,
	stateQuerier stateQuerier,
	ev roleValidator,
	validMsgsTracker pubKeyGetter,
	et *EligibilityTracker,
	logger log.Log,
) *syntaxContextValidator {
	return &syntaxContextValidator{
		signing:          sgr,
		pubKeyExtractor:  pubKeyExtractor,
		threshold:        threshold,
		statusValidator:  validator,
		stateQuerier:     stateQuerier,
		roleValidator:    ev,
		validMsgsTracker: validMsgsTracker,
		eTracker:         et,
		Log:              logger,
	}
}

// contextual validation errors.
var (
	errNilMsg         = errors.New("nil message")
	errNilInner       = errors.New("nil inner message")
	errEarlyMsg       = errors.New("early message")
	errInvalidIter    = errors.New("incorrect iteration number")
	errInvalidRound   = errors.New("incorrect round")
	errUnexpectedType = errors.New("unexpected message type")
)

// ContextuallyValidateMessage checks if the message is contextually valid.
// Returns nil if the message is contextually valid or a suitable error otherwise.
func (v *syntaxContextValidator) ContextuallyValidateMessage(ctx context.Context, m *Msg, currentK uint32) error {
	if m == nil {
		return errNilMsg
	}
	if m.InnerMessage == nil {
		return errNilInner
	}

	currentRound := currentK % RoundsPerIteration
	// the message must match the current iteration unless it is a notify or pre-round message
	currentIteration := currentK / RoundsPerIteration
	msgIteration := m.Round / RoundsPerIteration
	sameIter := currentIteration == msgIteration

	// first validate pre-round and notify
	switch m.Type {
	case pre:
		return nil
	case notify:
		if currentK == preRound && msgIteration != 0 {
			return errInvalidIter
		} else if currentK == preRound {
			return errInvalidRound
		}
		// notify before notify could be created for this iteration
		if currentRound < commitRound && sameIter {
			return errInvalidRound
		}

		// old notify is accepted
		if m.Round <= currentK {
			return nil
		}

		// early notify detected
		if m.Round == currentK+1 && currentRound == commitRound {
			return errEarlyMsg
		}

		// future notify is rejected
		return errInvalidIter
	}

	// check status, proposal & commit types
	switch m.Type {
	case status:
		if currentK == preRound && msgIteration != 0 {
			return errInvalidIter
		} else if currentK == preRound {
			return errEarlyMsg
		}
		if currentRound == notifyRound && currentIteration+1 == msgIteration {
			return errEarlyMsg
		}
		if currentRound == statusRound && sameIter {
			return nil
		}
		if !sameIter {
			return errInvalidIter
		}
		return errInvalidRound
	case proposal:
		if currentK == preRound && msgIteration != 0 {
			return errInvalidIter
		} else if currentK == preRound {
			return errInvalidRound
		}
		if currentRound == statusRound && sameIter {
			return errEarlyMsg
		}
		// a late proposal is also contextually valid
		if (currentRound == proposalRound || currentRound == commitRound) && sameIter {
			return nil
		}
		if !sameIter {
			return errInvalidIter
		}
		return errInvalidRound
	case commit:
		if currentK == preRound && msgIteration != 0 {
			return errInvalidIter
		} else if currentK == preRound {
			return errInvalidRound
		}
		if currentRound == proposalRound && sameIter {
			return errEarlyMsg
		}
		if currentRound == commitRound && sameIter {
			return nil
		}
		if !sameIter {
			return errInvalidIter
		}
		return errInvalidRound
	}

	return errUnexpectedType
}

// SyntacticallyValidateMessage the syntax of the provided message.
func (v *syntaxContextValidator) SyntacticallyValidateMessage(ctx context.Context, m *Msg) bool {
	logger := v.WithContext(ctx)

	if m == nil {
		logger.Warning("syntax validation failed: m is nil")
		return false
	}

	if m.NodeID == types.EmptyNodeID {
		logger.Warning("syntax validation failed: missing public key")
		return false
	}

	if m.InnerMessage == nil {
		logger.With().Warning("syntax validation failed: inner message is nil",
			log.Stringer("smesher", m.NodeID),
		)
		return false
	}

	claimedRound := m.Round % RoundsPerIteration
	switch m.Type {
	case pre:
		return true
	case status:
		return claimedRound == statusRound
	case proposal:
		return claimedRound == proposalRound && v.validateSVP(ctx, m)
	case commit:
		return claimedRound == commitRound
	case notify:
		return v.validateCertificate(ctx, m.Cert)
	default:
		logger.With().Warning("unknown message type encountered during syntactic validation",
			log.String("msg_type", m.Type.String()),
		)
		return false
	}
}

var (
	errNilValidators     = errors.New("validators param is nil")
	errNilAggMsgs        = errors.New("aggMsg is nil")
	errNilMsgsSlice      = errors.New("messages slice is nil")
	errMsgsCountMismatch = errors.New("number of messages does not match the threshold")
	errDupSender         = errors.New("duplicate sender detected")
	errInnerSyntax       = errors.New("invalid syntax for inner message")
	errInnerEligibility  = errors.New("inner message is not eligible")
	errInnerFunc         = errors.New("inner message did not pass validation function")
)

// validate the provided aggregated messages by the provided validators.
func (v *syntaxContextValidator) validateAggregatedMessage(ctx context.Context, aggMsg *AggregatedMessages, validators []func(m *Msg) bool) error {
	if validators == nil {
		return errNilValidators
	}

	if aggMsg == nil {
		return errNilAggMsgs
	}

	if len(aggMsg.Messages) == 0 {
		return errNilMsgsSlice
	}

	senders := make(map[types.NodeID]struct{})
	for _, innerMsg := range aggMsg.Messages {
		// check if exist in cache of valid messages
		if nodeID := v.validMsgsTracker.NodeID(&innerMsg); nodeID != types.EmptyNodeID {
			// validate unique sender
			if _, exist := senders[nodeID]; exist {
				return errDupSender
			}
			senders[nodeID] = struct{}{}

			// passed validation, continue to next message
			continue
		}

		// extract public key
		nodeId, err := v.pubKeyExtractor.ExtractNodeID(signing.HARE, innerMsg.SignedBytes(), innerMsg.Signature)
		if err != nil {
			return fmt.Errorf("extract ed25519 pubkey: %w", err)
		}

		// validate unique sender
		if _, exist := senders[nodeId]; exist { // pub already exist
			return errDupSender
		}
		senders[nodeId] = struct{}{} // mark sender as exist

		iMsg, err := newMsg(ctx, v.Log, nodeId, innerMsg, v.stateQuerier)
		if err != nil {
			return fmt.Errorf("new message: %w", err)
		}

		// validate with attached validators
		for _, vFunc := range validators {
			if !vFunc(iMsg) {
				return errInnerFunc
			}
		}

		if !v.SyntacticallyValidateMessage(ctx, iMsg) {
			return errInnerSyntax
		}

		// validate role
		if !v.roleValidator.Validate(ctx, iMsg) {
			return errInnerEligibility
		}
		v.eTracker.Track(iMsg.NodeID, iMsg.Round, iMsg.Eligibility.Count, true)

		// the message is valid, track it
		v.validMsgsTracker.Track(iMsg)
	}

	var ci CountInfo
	v.eTracker.ForEach(aggMsg.Messages[0].Round, func(node types.NodeID, cr *Cred) {
		// only counts the eligibility count from seen msgs
		if _, ok := senders[node]; ok {
			if cr.Honest {
				ci.IncHonest(cr.Count)
			} else {
				ci.IncDishonest(cr.Count)
			}
		} else if !cr.Honest {
			ci.IncKnownEquivocator(cr.Count)
		}
	})

	if ci.Meet(v.threshold) {
		if ci.numDishonest > 0 {
			v.Log.With().Warning("counting votes from malicious identities in aggregated messages",
				log.Object("eligibility_count", &ci))
		}
		return nil
	}
	return fmt.Errorf("%w: expected %v, actual dishonest %v honest %v",
		errMsgsCountMismatch, v.threshold, ci.dhCount, ci.hCount)
}

func (v *syntaxContextValidator) validateSVP(ctx context.Context, msg *Msg) bool {
	logger := v.WithContext(ctx)

	defer func(startTime time.Time) {
		logger.With().Debug("svp validation duration",
			log.String("duration", time.Since(startTime).String()))
	}(time.Now())
	proposalIter := inferIteration(msg.Round)
	validateSameIteration := func(m *Msg) bool {
		statusIter := inferIteration(m.Round)
		if proposalIter != statusIter { // not same iteration
			logger.With().Warning("proposal validation failed: not same iteration",
				log.Stringer("smesher", m.NodeID),
				m.Layer,
				log.Uint32("expected", proposalIter),
				log.Uint32("actual", statusIter))
			return false
		}

		return true
	}
	logger = logger.WithFields(log.Stringer("smesher", msg.NodeID), msg.Layer)
	validators := []func(m *Msg) bool{validateStatusType, validateSameIteration, v.statusValidator}
	if err := v.validateAggregatedMessage(ctx, msg.Svp, validators); err != nil {
		logger.With().Warning("invalid proposal", log.Err(err))
		return false
	}

	maxCommittedRound := preRound
	var maxSet []types.ProposalID
	for _, status := range msg.Svp.Messages {
		// track max
		if status.CommittedRound > maxCommittedRound || maxCommittedRound == preRound {
			maxCommittedRound = status.CommittedRound
			maxSet = status.Values
		}
	}

	if maxCommittedRound == preRound { // type A
		if !v.validateSVPTypeA(ctx, msg) {
			logger.Warning("proposal validation failed: type a validation failed")
			return false
		}
	} else {
		if !v.validateSVPTypeB(ctx, msg, NewSet(maxSet)) { // type B
			logger.Warning("proposal validation failed: type b validation failed")
			return false
		}
	}

	return true
}

func (v *syntaxContextValidator) validateCertificate(ctx context.Context, cert *Certificate) bool {
	logger := v.WithContext(ctx)

	defer func(startTime time.Time) {
		logger.With().Debug("certificate validation duration",
			log.String("duration", time.Since(startTime).String()))
	}(time.Now())

	if cert == nil {
		logger.Warning("certificate validation failed: certificate is nil")
		return false
	}

	if cert.AggMsgs == nil {
		logger.Warning("certificate validation failed: AggMsgs is nil")
		return false
	}

	// refill Values
	for _, commit := range cert.AggMsgs.Messages {
		if commit.InnerMessage == nil {
			logger.Warning("certificate validation failed: inner commit message is nil")
			return false
		}

		// the values were removed to reduce data volume
		commit.Values = cert.Values
	}

	// Note: no need to validate notify.Values=commits.Values because we refill the InnerMsg with notify.Values
	validateSameK := func(m *Msg) bool { return m.Round == cert.AggMsgs.Messages[0].Round }
	validators := []func(m *Msg) bool{validateCommitType, validateSameK}
	if err := v.validateAggregatedMessage(ctx, cert.AggMsgs, validators); err != nil {
		logger.With().Warning("invalid certificate", log.Err(err))
		return false
	}

	return true
}

func validateCommitType(m *Msg) bool {
	return m.Type == commit
}

func validateStatusType(m *Msg) bool {
	return m.Type == status
}

// validate SVP for type A (where all Ki=-1).
func (v *syntaxContextValidator) validateSVPTypeA(ctx context.Context, m *Msg) bool {
	s := NewSet(m.Values)
	unionSet := NewEmptySet(len(m.Values))
	for _, status := range m.Svp.Messages {
		statusSet := NewSet(status.Values)
		// build union
		for _, val := range statusSet.ToSlice() {
			unionSet.Add(val) // assuming add is unique
		}
	}

	if !unionSet.Equals(s) { // s should be the union of all statuses
		v.WithContext(ctx).With().Warning("proposal type a validation failed: not a union",
			log.String("expected", s.String()),
			log.String("actual", unionSet.String()),
		)
		return false
	}

	return true
}

// validate SVP for type B (where exist Ki>=0).
func (v *syntaxContextValidator) validateSVPTypeB(ctx context.Context, msg *Msg, maxSet *Set) bool {
	// max set should be equal to the claimed set
	s := NewSet(msg.Values)
	if !s.Equals(maxSet) {
		v.WithContext(ctx).With().Warning("proposal type b validation failed: max set not equal to proposed set",
			log.String("expected", s.String()),
			log.String("actual", maxSet.String()),
		)
		return false
	}

	return true
}
