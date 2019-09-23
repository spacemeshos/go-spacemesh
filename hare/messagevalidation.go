package hare

import (
	"errors"
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/log"
)

type messageValidator interface {
	SyntacticallyValidateMessage(m *Msg) bool
	ContextuallyValidateMessage(m *Msg, expectedK int32) error
}

type IdentityProvider interface {
	GetIdentity(edId string) (types.NodeId, error)
}

type eligibilityValidator struct {
	oracle           Rolacle
	layersPerEpoch   uint16
	identityProvider IdentityProvider
	maxExpActives    int // the maximal expected committee size
	expLeaders       int // the expected number of leaders
	log.Log
}

func NewEligibilityValidator(oracle Rolacle, layersPerEpoch uint16, idProvider IdentityProvider, maxExpActives, expLeaders int, logger log.Log) *eligibilityValidator {
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
	layer := types.LayerID(m.InnerMsg.InstanceId)
	if layer.GetEpoch(ev.layersPerEpoch).IsGenesis() {
		return true, nil // TODO: remove this lie after inception problem is addressed
	}

	nId, err := ev.identityProvider.GetIdentity(pub.String())
	if err != nil {
		ev.With().Error("Eligibility validator: GetIdentity failed (ignore if the safe layer is in genesis)", log.Err(err), log.String("sender_id", pub.ShortString()))
		return false, err
	}

	// validate role
	res, err := ev.oracle.Eligible(layer, m.InnerMsg.K, expectedCommitteeSize(m.InnerMsg.K, ev.maxExpActives, ev.expLeaders), nId, m.InnerMsg.RoleProof)
	if err != nil {
		ev.With().Error("Eligibility validator: could not retrieve eligibility result", log.Err(err))
		return false, err
	}
	if !res {
		ev.With().Error("Eligibility validator: sender is not eligible to participate", log.String("sender_pub", pub.ShortString()))
		return false, nil
	}

	return true, nil
}

// Validate the eligibility of the provided message.
func (ev *eligibilityValidator) Validate(m *Msg) bool {
	res, err := ev.validateRole(m)
	if err != nil {
		ev.Error("Error occurred while validating role err=%v", err)
		return false
	}
	// verify role
	if !res {
		ev.Warning("Validate message failed: role is invalid for pub %v", m.PubKey.ShortString())
		return false
	}

	return true
}

type syntaxContextValidator struct {
	signing         Signer
	threshold       int
	statusValidator func(m *Msg) bool // used to validate status Messages in SVP
	stateQuerier    StateQuerier
	layersPerEpoch  uint16
	log.Log
}

func newSyntaxContextValidator(signing Signer, threshold int, validator func(m *Msg) bool, stateQuerier StateQuerier, layersPerEpoch uint16, logger log.Log) *syntaxContextValidator {
	return &syntaxContextValidator{signing, threshold, validator, stateQuerier, layersPerEpoch, logger}
}

// contextual validation errors
var (
	errNilInner       = errors.New("nil inner message")
	errEarlyMsg       = errors.New("early message")
	errInvalidIter    = errors.New("incorrect iteration number")
	errInvalidRound   = errors.New("incorrect round")
	errUnexpectedType = errors.New("unexpected message type")
)

// ContextuallyValidateMessage checks if the message is contextually valid.
// Returns nil if the message is contextually valid or a suitable error otherwise.
// Note: we assume m is syntactically valid (int that case, m.InnerMsg.K is a valid expression).
func (validator *syntaxContextValidator) ContextuallyValidateMessage(m *Msg, currentK int32) error {
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
func (validator *syntaxContextValidator) SyntacticallyValidateMessage(m *Msg) bool {
	if m == nil {
		validator.Warning("Syntax validation failed: m is nil")
		return false
	}

	if m.PubKey == nil {
		validator.Warning("Syntax validation failed: missing public key")
		return false
	}

	if m.InnerMsg == nil {
		validator.Warning("Syntax validation failed: inner message is nil")
		return false
	}

	if m.InnerMsg.Values == nil {
		validator.Warning("Syntax validation failed: Values is nil in msg: %v", m.String())
		return false
	}

	if len(m.InnerMsg.Values) == 0 {
		validator.Warning("Syntax validation failed: Values is empty: %v", m.String())
		return false
	}

	claimedRound := m.InnerMsg.K % 4
	switch m.InnerMsg.Type {
	case pre:
		return true
	case status:
		return claimedRound == statusRound
	case proposal:
		return claimedRound == proposalRound && validator.validateSVP(m)
	case commit:
		return claimedRound == commitRound
	case notify:
		return validator.validateCertificate(m.InnerMsg.Cert)
	default:
		validator.Error("Unknown message type encountered during syntactic validator: ", m.InnerMsg.Type)
		return false
	}
}

// validate the provided aggregated messages by the provided validators.
func (validator *syntaxContextValidator) validateAggregatedMessage(aggMsg *aggregatedMessages, validators []func(m *Msg) bool) bool {
	if validators == nil {
		validator.Error("Aggregated validation failed: validators param is nil")
		return false
	}

	if aggMsg == nil {
		validator.Warning("Aggregated validation failed: aggMsg is nil")
		return false
	}

	if aggMsg.Messages == nil { // must contain status Messages
		validator.Warning("Aggregated validation failed: Messages slice is nil")
		return false
	}

	if len(aggMsg.Messages) != validator.threshold { // must include exactly f+1 Messages
		validator.Warning("Aggregated validation failed: number of Messages does not match. Expected: %v Actual: %v",
			validator.threshold, len(aggMsg.Messages))
		return false
	}

	// TODO: validate agg sig

	senders := make(map[string]struct{})
	for _, innerMsg := range aggMsg.Messages {

		iMsg, err := newMsg(innerMsg, validator.stateQuerier, validator.layersPerEpoch)
		if err != nil {
			validator.Warning("Aggregated validation failed: could not construct msg err=%v", err)
			return false
		}

		if !validator.SyntacticallyValidateMessage(iMsg) {
			validator.Warning("Aggregated validation failed: identified an invalid inner message %v", iMsg)
			return false
		}

		// validate unique sender
		pub := iMsg.PubKey
		if _, exist := senders[pub.String()]; exist { // pub already exist
			validator.Warning("Aggregated validation failed: detected same pubKey for different Messages")
			return false
		}
		senders[pub.String()] = struct{}{} // mark sender as exist

		// validate with attached validators
		for _, vFunc := range validators {
			if !vFunc(iMsg) {
				validator.Warning("Aggregated validation failed: attached vFunc failed")
				return false
			}
		}
	}

	return true
}

func (validator *syntaxContextValidator) validateSVP(msg *Msg) bool {
	validateSameIteration := func(m *Msg) bool {
		proposalIter := iterationFromCounter(msg.InnerMsg.K)
		statusIter := iterationFromCounter(m.InnerMsg.K)
		if proposalIter != statusIter { // not same iteration
			validator.Warning("Proposal validation failed: not same iteration. Expected: %v Actual: %v",
				proposalIter, statusIter)
			return false
		}

		return true
	}
	validators := []func(m *Msg) bool{validateStatusType, validateSameIteration, validator.statusValidator}
	if !validator.validateAggregatedMessage(msg.InnerMsg.Svp, validators) {
		validator.Warning("Proposal validation failed: failed to validate aggregated message")
		return false
	}

	maxKi := int32(-1) // Ki>=-1
	var maxSet []uint64 = nil
	for _, status := range msg.InnerMsg.Svp.Messages {
		// track max
		if status.InnerMsg.Ki > maxKi {
			maxKi = status.InnerMsg.Ki
			maxSet = status.InnerMsg.Values
		}
	}

	if maxKi == -1 { // type A
		if !validator.validateSVPTypeA(msg) {
			validator.Warning("Proposal validation failed: type A validation failed")
			return false
		}
	} else {
		if !validator.validateSVPTypeB(msg, NewSet(maxSet)) { // type B
			validator.Warning("Proposal validation failed: type B validation failed")
			return false
		}
	}

	return true
}

func (validator *syntaxContextValidator) validateCertificate(cert *certificate) bool {
	if cert == nil {
		validator.Warning("Certificate validation failed: certificate is nil")
		return false
	}

	// verify agg msgs
	if cert.AggMsgs == nil {
		validator.Warning("Certificate validation failed: AggMsgs is nil")
		return false
	}

	// refill Values
	for _, commit := range cert.AggMsgs.Messages {
		if commit.InnerMsg == nil {
			validator.Warning("Certificate validation failed: inner commit message is nil")
			return false
		}

		commit.InnerMsg.Values = cert.Values
	}

	// Note: no need to validate notify.Values=commits.Values because we refill the InnerMsg with notify.Values
	validateSameK := func(m *Msg) bool { return m.InnerMsg.K == cert.AggMsgs.Messages[0].InnerMsg.K }
	validators := []func(m *Msg) bool{validateCommitType, validateSameK}
	if !validator.validateAggregatedMessage(cert.AggMsgs, validators) {
		validator.Warning("Certificate validation failed: aggregated Messages validation failed")
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
func (validator *syntaxContextValidator) validateSVPTypeA(m *Msg) bool {
	s := NewSet(m.InnerMsg.Values)
	unionSet := NewEmptySet(len(m.InnerMsg.Values))
	for _, status := range m.InnerMsg.Svp.Messages {
		statusSet := NewSet(status.InnerMsg.Values)
		// build union
		for _, buff := range statusSet.values {
			bid := buff
			unionSet.Add(bid) // assuming add is unique
		}
	}

	if !unionSet.Equals(s) { // s should be the union of all statuses
		validator.Warning("Proposal type A validation failed: not a union. Expected: %v Actual: %v", s, unionSet)
		return false
	}

	return true
}

// validate SVP for type B (where exist Ki>=0)
func (validator *syntaxContextValidator) validateSVPTypeB(msg *Msg, maxSet *Set) bool {
	// max set should be equal to the claimed set
	s := NewSet(msg.InnerMsg.Values)
	if !s.Equals(maxSet) {
		validator.Warning("Proposal type B validation failed: max set not equal to proposed set. Expected: %v Actual: %v", s, maxSet)
		return false
	}

	return true
}
