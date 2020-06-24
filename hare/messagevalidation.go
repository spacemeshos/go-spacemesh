package hare

import (
	"errors"
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/signing"
	"time"
)

type messageValidator interface {
	SyntacticallyValidateMessage(m *Msg) bool
	ContextuallyValidateMessage(m *Msg, expectedK int32) error
}

type identityProvider interface {
	GetIdentity(edID string) (types.NodeID, error)
}

type eligibilityValidator struct {
	oracle           Rolacle
	layersPerEpoch   uint16
	identityProvider identityProvider
	maxExpActives    int // the maximal expected committee size
	expLeaders       int // the expected number of leaders
	log.Log
}

func newEligibilityValidator(oracle Rolacle, layersPerEpoch uint16, idProvider identityProvider, maxExpActives, expLeaders int, logger log.Log) *eligibilityValidator {
	return &eligibilityValidator{oracle, layersPerEpoch, idProvider, maxExpActives, expLeaders, logger}
}

// check eligibility of the provided message by the oracle.
func (ev *eligibilityValidator) validateRole(m *Msg) (bool, error) {
	if m == nil {
		ev.Error("Eligibility validator: called with nil")
		return false, errors.New("fatal: nil message")
	}

	if m.InnerMsg == nil {
		ev.Error("Eligibility validator: InnerMsg is nil")
		return false, errors.New("fatal: nil inner message")
	}

	pub := m.PubKey
	layer := types.LayerID(m.InnerMsg.InstanceID)
	if layer.GetEpoch(ev.layersPerEpoch).IsGenesis() {
		return true, nil // TODO: remove this lie after inception problem is addressed
	}

	nID, err := ev.identityProvider.GetIdentity(pub.String())
	if err != nil {
		ev.With().Error("Eligibility validator: GetIdentity failed (ignore if the safe layer is in genesis)", log.Err(err), log.String("sender_id", pub.ShortString()))
		return false, err
	}

	// validate role
	res, err := ev.oracle.Eligible(layer, m.InnerMsg.K, expectedCommitteeSize(m.InnerMsg.K, ev.maxExpActives, ev.expLeaders), nID, m.InnerMsg.RoleProof)
	if err != nil {
		ev.With().Error("Eligibility validator: could not retrieve eligibility result", log.Err(err), log.String("sender_id", pub.ShortString()))
		return false, err
	}
	if !res {
		ev.With().Error("Eligibility validator: sender is not eligible to participate", log.String("sender_id", pub.ShortString()))
		return false, nil
	}

	return true, nil
}

// Validate the eligibility of the provided message.
func (ev *eligibilityValidator) Validate(m *Msg) bool {
	res, err := ev.validateRole(m)
	if err != nil {
		ev.With().Error("Error occurred while validating role", log.Err(err), log.String("sender_id", m.PubKey.ShortString()),
			types.LayerID(m.InnerMsg.InstanceID), log.String("msg_type", m.InnerMsg.Type.String()))
		return false
	}
	// verify role
	if !res {
		ev.With().Warning("Validate message failed: role is invalid", log.String("sender_id", m.PubKey.ShortString()),
			types.LayerID(m.InnerMsg.InstanceID), log.String("msg_type", m.InnerMsg.Type.String()))
		return false
	}

	return true
}

type roleValidator interface {
	Validate(m *Msg) bool
}

type pubKeyGetter interface {
	Track(m *Msg)
	PublicKey(m *Message) *signing.PublicKey
}

type syntaxContextValidator struct {
	signing          Signer
	threshold        int
	statusValidator  func(m *Msg) bool // used to validate status Messages in SVP
	stateQuerier     StateQuerier
	layersPerEpoch   uint16
	roleValidator    roleValidator
	validMsgsTracker pubKeyGetter // used to check for public keys in the valid messages tracker
	log.Log
}

func newSyntaxContextValidator(sgr Signer, threshold int, validator func(m *Msg) bool, stateQuerier StateQuerier, layersPerEpoch uint16, ev roleValidator, validMsgsTracker pubKeyGetter, logger log.Log) *syntaxContextValidator {
	return &syntaxContextValidator{sgr, threshold, validator, stateQuerier, layersPerEpoch, ev, validMsgsTracker, logger}
}

// contextual validation errors
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
func (v *syntaxContextValidator) ContextuallyValidateMessage(m *Msg, currentK int32) error {
	if m == nil {
		return errNilMsg
	}

	if m.InnerMsg == nil {
		return errNilInner
	}
	currentRound := currentK % 4
	// the message must match the current iteration unless it is a notify or pre-round message
	currentIteration := currentK / 4
	msgIteration := m.InnerMsg.K / 4
	sameIter := currentIteration == msgIteration

	// first validate pre-round and notify
	switch m.InnerMsg.Type {
	case pre:
		return nil
	case notify:
		// notify before notify could be created for this iteration
		if currentRound < commitRound && sameIter {
			return errInvalidRound
		}

		// old notify is accepted
		if m.InnerMsg.K <= currentK {
			return nil
		}

		// early notify detected
		if m.InnerMsg.K == currentK+1 && currentRound == commitRound {
			return errEarlyMsg
		}

		// future notify is rejected
		return errInvalidIter
	}

	// check status, proposal & commit types
	switch m.InnerMsg.Type {
	case status:
		if currentRound == preRound && sameIter {
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
func (v *syntaxContextValidator) SyntacticallyValidateMessage(m *Msg) bool {
	if m == nil {
		v.Warning("Syntax validation failed: m is nil")
		return false
	}

	if m.PubKey == nil {
		v.Warning("Syntax validation failed: missing public key")
		return false
	}

	if m.InnerMsg == nil {
		v.Warning("Syntax validation failed: inner message is nil")
		return false
	}

	if m.InnerMsg.Values == nil {
		v.With().Warning("Syntax validation failed: set is nil",
			log.String("sender_id", m.PubKey.ShortString()), types.LayerID(m.InnerMsg.InstanceID), log.String("msg_type", m.InnerMsg.Type.String()))
		return false
	}

	if len(m.InnerMsg.Values) == 0 {
		v.With().Warning("Syntax validation failed: set is empty",
			log.String("sender_id", m.PubKey.ShortString()), types.LayerID(m.InnerMsg.InstanceID), log.String("msg_type", m.InnerMsg.Type.String()))
		return false
	}

	claimedRound := m.InnerMsg.K % 4
	switch m.InnerMsg.Type {
	case pre:
		return true
	case status:
		return claimedRound == statusRound
	case proposal:
		return claimedRound == proposalRound && v.validateSVP(m)
	case commit:
		return claimedRound == commitRound
	case notify:
		return v.validateCertificate(m.InnerMsg.Cert)
	default:
		v.Error("Unknown message type encountered during syntactic validator: ", m.InnerMsg.Type)
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
func (v *syntaxContextValidator) validateAggregatedMessage(aggMsg *aggregatedMessages, validators []func(m *Msg) bool) error {
	if validators == nil {
		return errNilValidators
	}

	if aggMsg == nil {
		return errNilAggMsgs
	}

	if aggMsg.Messages == nil { // must contain status Messages
		return errNilMsgsSlice
	}

	if len(aggMsg.Messages) != v.threshold { // must include exactly f+1 Messages
		v.Warning("Aggregated validation failed: number of messages does not match. Expected: %v Actual: %v",
			v.threshold, len(aggMsg.Messages))
		return errMsgsCountMismatch
	}

	senders := make(map[string]struct{})
	for _, innerMsg := range aggMsg.Messages {

		// check if exist in cache of valid messages
		if pub := v.validMsgsTracker.PublicKey(innerMsg); pub != nil {
			// validate unique sender
			if _, exist := senders[pub.String()]; exist { // pub already exist
				return errDupSender
			}
			senders[pub.String()] = struct{}{} // mark sender as exist

			// passed validation, continue to next message
			continue
		}

		// extract public key
		iMsg, err := newMsg(innerMsg, v.stateQuerier)
		if err != nil {
			return err
		}

		pub := iMsg.PubKey
		// validate unique sender
		if _, exist := senders[pub.String()]; exist { // pub already exist
			return errDupSender
		}
		senders[pub.String()] = struct{}{} // mark sender as exist

		if !v.SyntacticallyValidateMessage(iMsg) {
			return errInnerSyntax
		}

		// validate role
		if !v.roleValidator.Validate(iMsg) {
			return errInnerEligibility
		}

		// validate with attached validators
		for _, vFunc := range validators {
			if !vFunc(iMsg) {
				return errInnerFunc
			}
		}

		// the message is valid, track it
		v.validMsgsTracker.Track(iMsg)
	}

	return nil
}

func (v *syntaxContextValidator) validateSVP(msg *Msg) bool {
	defer func(startTime time.Time) {
		v.With().Debug("SVP validation duration", log.String("duration", time.Now().Sub(startTime).String()))
	}(time.Now())
	proposalIter := iterationFromCounter(msg.InnerMsg.K)
	validateSameIteration := func(m *Msg) bool {
		statusIter := iterationFromCounter(m.InnerMsg.K)
		if proposalIter != statusIter { // not same iteration
			v.With().Warning("Proposal validation failed: not same iteration",
				log.String("sender_id", m.PubKey.ShortString()), types.LayerID(m.InnerMsg.InstanceID),
				log.Int32("expected", proposalIter), log.Int32("actual", statusIter))
			return false
		}

		return true
	}
	validators := []func(m *Msg) bool{validateStatusType, validateSameIteration, v.statusValidator}
	if err := v.validateAggregatedMessage(msg.InnerMsg.Svp, validators); err != nil {
		v.With().Warning("Proposal validation failed: failed to validate aggregated messages", log.Err(err),
			log.String("sender_id", msg.PubKey.ShortString()), types.LayerID(msg.InnerMsg.InstanceID))
		return false
	}

	maxKi := int32(-1) // Ki>=-1
	var maxSet []types.BlockID
	for _, status := range msg.InnerMsg.Svp.Messages {
		// track max
		if status.InnerMsg.Ki > maxKi {
			maxKi = status.InnerMsg.Ki
			maxSet = status.InnerMsg.Values
		}
	}

	if maxKi == -1 { // type A
		if !v.validateSVPTypeA(msg) {
			v.Warning("Proposal validation failed: type A validation failed",
				log.String("sender_id", msg.PubKey.ShortString()), types.LayerID(msg.InnerMsg.InstanceID))
			return false
		}
	} else {
		if !v.validateSVPTypeB(msg, NewSet(maxSet)) { // type B
			v.Warning("Proposal validation failed: type B validation failed",
				log.String("sender_id", msg.PubKey.ShortString()), types.LayerID(msg.InnerMsg.InstanceID))
			return false
		}
	}

	return true
}

func (v *syntaxContextValidator) validateCertificate(cert *certificate) bool {
	defer func(startTime time.Time) {
		v.With().Debug("certificate validation duration", log.String("duration", time.Now().Sub(startTime).String()))
	}(time.Now())

	if cert == nil {
		v.Warning("Certificate validation failed: certificate is nil")
		return false
	}

	// verify agg msgs
	if cert.AggMsgs == nil {
		v.Warning("Certificate validation failed: AggMsgs is nil")
		return false
	}

	// refill Values
	for _, commit := range cert.AggMsgs.Messages {
		if commit.InnerMsg == nil {
			v.Warning("Certificate validation failed: inner commit message is nil")
			return false
		}

		commit.InnerMsg.Values = cert.Values
	}

	// Note: no need to validate notify.Values=commits.Values because we refill the InnerMsg with notify.Values
	validateSameK := func(m *Msg) bool { return m.InnerMsg.K == cert.AggMsgs.Messages[0].InnerMsg.K }
	validators := []func(m *Msg) bool{validateCommitType, validateSameK}
	if err := v.validateAggregatedMessage(cert.AggMsgs, validators); err != nil {
		v.With().Warning("Certificate validation failed: aggregated messages validation failed", log.Err(err))
		return false
	}

	return true
}

func validateCommitType(m *Msg) bool {
	return messageType(m.InnerMsg.Type) == commit
}

func validateStatusType(m *Msg) bool {
	return messageType(m.InnerMsg.Type) == status
}

// validate SVP for type A (where all Ki=-1)
func (v *syntaxContextValidator) validateSVPTypeA(m *Msg) bool {
	s := NewSet(m.InnerMsg.Values)
	unionSet := NewEmptySet(len(m.InnerMsg.Values))
	for _, status := range m.InnerMsg.Svp.Messages {
		statusSet := NewSet(status.InnerMsg.Values)
		// build union
		for bid := range statusSet.values {
			unionSet.Add(bid) // assuming add is unique
		}
	}

	if !unionSet.Equals(s) { // s should be the union of all statuses
		v.Warning("Proposal type A validation failed: not a union. Expected: %v Actual: %v", s, unionSet)
		return false
	}

	return true
}

// validate SVP for type B (where exist Ki>=0)
func (v *syntaxContextValidator) validateSVPTypeB(msg *Msg, maxSet *Set) bool {
	// max set should be equal to the claimed set
	s := NewSet(msg.InnerMsg.Values)
	if !s.Equals(maxSet) {
		v.Warning("Proposal type B validation failed: max set not equal to proposed set. Expected: %v Actual: %v", s, maxSet)
		return false
	}

	return true
}
