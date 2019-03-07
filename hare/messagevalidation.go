package hare

import (
	"github.com/gogo/protobuf/proto"
	"github.com/spacemeshos/go-spacemesh/hare/pb"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/mesh"
)

type messageValidator interface {
	SyntacticallyValidateMessage(m *pb.HareMessage) bool
	ContextuallyValidateMessage(m *pb.HareMessage, expectedK int32) bool
}

type eligibilityValidator struct {
	oracle HareRolacle
	log.Log
}

func newEligibilityValidator(oracle HareRolacle, logger log.Log) *eligibilityValidator {
	return &eligibilityValidator{oracle, logger}
}

func (ev *eligibilityValidator) validateRole(m *pb.HareMessage) bool {
	if m == nil {
		ev.Error("Eligibility validator: called with nil")
		return false
	}

	if m.Message == nil {
		ev.Warning("Eligibility validator: message is nil")
		return false
	}

	// TODO: validate role proof sig

	verifier, err := NewVerifier(m.PubKey)
	if err != nil {
		ev.Error("Could not build verifier")
		return false
	}

	// validate role
	if !ev.oracle.Eligible(InstanceId(m.Message.InstanceId), m.Message.K, verifier.String(), Signature(m.Message.RoleProof)) {
		ev.Warning("Role validation failed")
		return false
	}

	return true
}

// Validates eligibility and signature of the provided message
func (ev *eligibilityValidator) Validate(m *pb.HareMessage) bool {
	data, err := proto.Marshal(m.Message)
	if err != nil {
		ev.Error("Validate message failed: failed marshaling inner message")
		return false
	}

	// verify signature
	verifier, err := NewVerifier(m.PubKey)
	if err != nil {
		ev.Warning("Validate message failed: Could not construct verifier ", err)
		return false
	}
	res, _ := verifier.Verify(data, m.InnerSig)
	if !res {
		ev.Warning("Validate message failed: invalid message signature detected ")
		return false
	}

	// verify role
	if !ev.validateRole(m) {
		ev.Warning("Validate message failed: role is invalid")
		return false
	}

	return true
}

type syntaxContextValidator struct {
	signing         Signing
	threshold       int
	statusValidator func(m *pb.HareMessage) bool // used to validate status messages in SVP
	log.Log
}

func newSyntaxContextValidator(signing Signing, threshold int, validator func(m *pb.HareMessage) bool, logger log.Log) *syntaxContextValidator {
	return &syntaxContextValidator{signing, threshold, validator, logger}
}

// Validates the message is contextually valid
func (validator *syntaxContextValidator) ContextuallyValidateMessage(m *pb.HareMessage, expectedK int32) bool {
	if m.Message == nil {
		validator.Warning("Contextual validation failed: m.Message is nil")
		return false
	}

	// PreRound & Notify are always contextually valid
	switch MessageType(m.Message.Type) {
	case PreRound:
		return true
	case Notify:
		return true
	}

	// Status, Proposal, Commit messages should match the expected k
	if expectedK == m.Message.K {
		return true
	}

	validator.Info("Contextual validation failed: not same iteration. Expected: %v, Actual: %v", expectedK, m.Message.K)
	return false
}

// Validates the syntax of the provided message
func (validator *syntaxContextValidator) SyntacticallyValidateMessage(m *pb.HareMessage) bool {
	if m == nil {
		validator.Warning("Syntax validation failed: m is nil")
		return false
	}

	if m.PubKey == nil {
		validator.Warning("Syntax validation failed: missing public key")
		return false
	}

	if m.Message == nil {
		validator.Warning("Syntax validation failed: inner message is nil")
		return false
	}

	if m.Message.Values == nil {
		validator.Warning("Syntax validation failed: values is nil in msg: %v", m)
		return false
	}

	if len(m.Message.Values) == 0 {
		validator.Warning("Syntax validation failed: values is empty: %v", m)
		return false
	}

	claimedRound := m.Message.K % 4
	switch MessageType(m.Message.Type) {
	case PreRound:
		return true
	case Status:
		return claimedRound == Round1
	case Proposal:
		return claimedRound == Round2 && validator.validateSVP(m)
	case Commit:
		return claimedRound == Round3
	case Notify:
		return validator.validateCertificate(m.Cert)
	default:
		validator.Error("Unknown message type encountered during syntactic validator: ", m.Message.Type)
		return false
	}
}

func (validator *syntaxContextValidator) validateAggregatedMessage(aggMsg *pb.AggregatedMessages, validators []func(m *pb.HareMessage) bool) bool {
	if validators == nil {
		validator.Error("Aggregated validation failed: validators param is nil")
		return false
	}

	if aggMsg == nil {
		validator.Warning("Aggregated validation failed: aggMsg is nil")
		return false
	}

	if aggMsg.Messages == nil { // must contain status messages
		validator.Warning("Aggregated validation failed: messages slice is nil")
		return false
	}

	if len(aggMsg.Messages) != validator.threshold { // must include exactly f+1 messages
		validator.Warning("Aggregated validation failed: number of messages does not match. Expected: %v Actual: %v",
			validator.threshold, len(aggMsg.Messages))
		return false
	}

	// TODO: refill values in commit on certificate
	// TODO: validate agg sig

	senders := make(map[string]struct{})
	for _, innerMsg := range aggMsg.Messages {
		if !validator.SyntacticallyValidateMessage(innerMsg) {
			validator.Warning("Aggregated validation failed: identified an invalid inner message")
			return false
		}

		// validate unique sender
		verifier, err := NewVerifier(innerMsg.PubKey)
		if err != nil {
			validator.Warning("Aggregated validation failed: could not construct verifier: ", err)
			return false
		}
		if _, exist := senders[verifier.String()]; exist { // pub already exist
			validator.Warning("Aggregated validation failed: detected same pubKey for different messages")
			return false
		}
		senders[verifier.String()] = struct{}{} // mark sender as exist

		// validate with attached validators
		for _, vFunc := range validators {
			if !vFunc(innerMsg) {
				validator.Warning("Aggregated validation failed: attached vFunc failed")
				return false
			}
		}
	}

	return true
}

func (validator *syntaxContextValidator) validateSVP(msg *pb.HareMessage) bool {
	validateSameIteration := func(m *pb.HareMessage) bool {
		proposalIter := iterationFromCounter(msg.Message.K)
		statusIter := iterationFromCounter(m.Message.K)
		if proposalIter != statusIter { // not same iteration
			validator.Warning("Proposal validation failed: not same iteration. Expected: %v Actual: %v",
				proposalIter, statusIter)
			return false
		}

		return true
	}
	validators := []func(m *pb.HareMessage) bool{validateStatusType, validateSameIteration, validator.statusValidator}
	if !validator.validateAggregatedMessage(msg.Message.Svp, validators) {
		validator.Warning("Proposal validation failed: failed to validate aggregated message")
		return false
	}

	maxKi := int32(-1) // ki>=-1
	var maxRawSet []uint64 = nil
	for _, status := range msg.Message.Svp.Messages {
		// track max
		if status.Message.Ki > maxKi {
			maxKi = status.Message.Ki
			maxRawSet = status.Message.Values
		}
	}

	if maxKi == -1 { // type A
		if !validator.validateSVPTypeA(msg) {
			validator.Warning("Proposal validation failed: type A validation failed")
			return false
		}
	} else {
		if !validator.validateSVPTypeB(msg, NewSet(maxRawSet)) { // type B
			validator.Warning("Proposal validation failed: type B validation failed")
			return false
		}
	}

	return true
}

func (validator *syntaxContextValidator) validateCertificate(cert *pb.Certificate) bool {
	if cert == nil {
		validator.Warning("Certificate validation failed: certificate is nil")
		return false
	}

	// verify agg msgs
	if cert.AggMsgs == nil {
		validator.Warning("Certificate validation failed: AggMsgs is nil")
		return false
	}

	// refill values
	for _, commit := range cert.AggMsgs.Messages {
		if commit.Message == nil {
			validator.Warning("Certificate validation failed: inner commit message is nil")
			return false
		}

		commit.Message.Values = cert.Values
	}

	// Note: no need to validate notify.values=commits.values because we refill the message with notify.values
	validateSameK := func(m *pb.HareMessage) bool { return m.Message.K == cert.AggMsgs.Messages[0].Message.K }
	validators := []func(m *pb.HareMessage) bool{validateCommitType, validateSameK}
	if !validator.validateAggregatedMessage(cert.AggMsgs, validators) {
		validator.Warning("Certificate validation failed: aggregated messages validation failed")
		return false
	}

	return true
}

func validateCommitType(m *pb.HareMessage) bool {
	return MessageType(m.Message.Type) == Commit
}

func validateStatusType(m *pb.HareMessage) bool {
	return MessageType(m.Message.Type) == Status
}

// validate SVP for type A (where all ki=-1)
func (validator *syntaxContextValidator) validateSVPTypeA(m *pb.HareMessage) bool {
	s := NewSet(m.Message.Values)
	unionSet := NewEmptySet(cap(m.Message.Values))
	for _, status := range m.Message.Svp.Messages {
		// build union
		for _, buff := range status.Message.Values {
			bid := Value{mesh.BlockID(buff)}
			unionSet.Add(bid) // assuming add is unique
		}
	}

	if !unionSet.Equals(s) { // s should be the union of all statuses
		validator.Warning("Proposal type A validation failed: not a union. Expected: %v Actual: %v", s, unionSet)
		return false
	}

	return true
}

// validate SVP for type B (where exist ki>=0)
func (validator *syntaxContextValidator) validateSVPTypeB(msg *pb.HareMessage, maxSet *Set) bool {
	// max set should be equal to the claimed set
	s := NewSet(msg.Message.Values)
	if !s.Equals(maxSet) {
		validator.Warning("Proposal type B validation failed: max set not equal to proposed set. Expected: %v Actual: %v", s, maxSet)
		return false
	}

	return true
}
