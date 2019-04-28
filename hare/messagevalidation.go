package hare

import (
	"github.com/spacemeshos/go-spacemesh/log"
)

type messageValidator interface {
	SyntacticallyValidateMessage(m *Msg) bool
	ContextuallyValidateMessage(m *Msg, expectedK int32) bool
}

type eligibilityValidator struct {
	oracle HareRolacle
	log.Log
}

func NewEligibilityValidator(oracle HareRolacle, logger log.Log) *eligibilityValidator {
	return &eligibilityValidator{oracle, logger}
}

func (ev *eligibilityValidator) validateRole(m *Msg) bool {
	if m == nil {
		ev.Error("Eligibility validator: called with nil")
		return false
	}

	if m.InnerMsg == nil {
		ev.Warning("Eligibility validator: InnerMsg is nil")
		return false
	}

	// TODO: validate role proof sig

	pub := m.PubKey
	// validate role
	if !ev.oracle.Eligible(InstanceId(m.InnerMsg.InstanceId), m.InnerMsg.K, pub.String(), Signature(m.InnerMsg.RoleProof)) {
		ev.Warning("Role validation failed")
		return false
	}

	return true
}

// Validates eligibility and signature of the provided InnerMsg
func (ev *eligibilityValidator) Validate(m *Msg) bool {
	// verify role
	if !ev.validateRole(m) {
		ev.Warning("Validate message failed: role is invalid for pub %v", m.PubKey.ShortString())
		return false
	}

	return true
}

type syntaxContextValidator struct {
	signing         Signer
	threshold       int
	statusValidator func(m *Msg) bool // used to validate status Messages in SVP
	log.Log
}

func newSyntaxContextValidator(signing Signer, threshold int, validator func(m *Msg) bool, logger log.Log) *syntaxContextValidator {
	return &syntaxContextValidator{signing, threshold, validator, logger}
}

// Validates the InnerMsg is contextually valid
func (validator *syntaxContextValidator) ContextuallyValidateMessage(m *Msg, expectedK int32) bool {
	if m.InnerMsg == nil {
		validator.Warning("Contextual validation failed: m.InnerMsg is nil")
		return false
	}

	// PreRound & Notify are always contextually valid
	switch m.InnerMsg.Type {
	case PreRound:
		return true
	case Notify:
		return true
	}

	// Status, Proposal, Commit Messages should match the expected K
	if expectedK == m.InnerMsg.K {
		return true
	}

	validator.Info("Contextual validation failed: not same iteration. Expected: %v, Actual: %v", expectedK, m.InnerMsg.K)
	return false
}

// Validates the syntax of the provided InnerMsg
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
		validator.Warning("Syntax validation failed: Values is nil in msg: %v", m)
		return false
	}

	if len(m.InnerMsg.Values) == 0 {
		validator.Warning("Syntax validation failed: Values is empty: %v", m)
		return false
	}

	claimedRound := m.InnerMsg.K % 4
	switch m.InnerMsg.Type {
	case PreRound:
		return true
	case Status:
		return claimedRound == Round1
	case Proposal:
		return claimedRound == Round2 && validator.validateSVP(m)
	case Commit:
		return claimedRound == Round3
	case Notify:
		return validator.validateCertificate(m.InnerMsg.Cert)
	default:
		validator.Error("Unknown message type encountered during syntactic validator: ", m.InnerMsg.Type)
		return false
	}
}

func (validator *syntaxContextValidator) validateAggregatedMessage(aggMsg *AggregatedMessages, validators []func(m *Msg) bool) bool {
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
		// TODO: refill Values in commit on certificate

		// TODO: should receive the state querier
		iMsg, err := newMsg(innerMsg, MockStateQuerier{true, nil})
		if err != nil {
			validator.Warning("Aggregated validation failed: could not construct msg")
			return false
		}

		if !validator.SyntacticallyValidateMessage(iMsg) {
			validator.Warning("Aggregated validation failed: identified an invalid inner message %v", iMsg)
			return false
		}

		// validate unique sender
		pub := iMsg.PubKey
		if err != nil {
			validator.Warning("Aggregated validation failed: could not construct pub: %v", err)
			return false
		}
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

func (validator *syntaxContextValidator) validateCertificate(cert *Certificate) bool {
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
	return MessageType(m.InnerMsg.Type) == Commit
}

func validateStatusType(m *Msg) bool {
	return MessageType(m.InnerMsg.Type) == Status
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
