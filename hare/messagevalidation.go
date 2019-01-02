package hare

import (
	"github.com/spacemeshos/go-spacemesh/crypto"
	"github.com/spacemeshos/go-spacemesh/hare/pb"
	"github.com/spacemeshos/go-spacemesh/log"
)

type MessageValidator struct {
	threshold   int
	defaultSize int
}

func NewMessageValidator(threshold int, defaultSize int) *MessageValidator {
	return &MessageValidator{threshold, defaultSize}
}

func (validator *MessageValidator) validateNotify(m *pb.HareMessage) bool {
	return validator.validateCertificate(m.Cert)
}

// verifies the message is syntactically valid
func (validator *MessageValidator) IsSyntacticallyValid(m *pb.HareMessage) bool {
	if m == nil || m.PubKey == nil || m.Message == nil || m.Message.Values == nil {
		log.Warning("Syntax validation failed: nil identified")
		return false
	}

	claimedRound := m.Message.K % 4
	switch MessageType(m.Message.Type) {
	case PreRound:
		return true
	case Status:
		return claimedRound == Round1
	case Proposal:
		return claimedRound == Round2 && validator.validateProposal(m)
	case Commit:
		return claimedRound == Round3
	case Notify:
		return validator.validateNotify(m)
	default:
		log.Error("Unknown message type encountered during syntactic validator: ", m.Message.Type)
		return false
	}
}

func (validator *MessageValidator) validateAggregatedMessage(aggMsg *pb.AggregatedMessages, validators []func(m *pb.HareMessage) bool) bool {
	if validators == nil {
		log.Error("Aggregated validation failed: validators param is nil")
		return false
	}

	if aggMsg == nil {
		log.Warning("Aggregated validation failed: aggMsg is nil")
		return false
	}

	if aggMsg.Messages == nil { // must contain status messages
		log.Warning("Aggregated validation failed: messages slice is nil")
		return false
	}

	if len(aggMsg.Messages) != validator.threshold { // must include exactly f+1 messages
		log.Warning("Aggregated validation failed: number of messages does not match. Expected: %v Actual: %v",
			validator.threshold, len(aggMsg.Messages))
		return false
	}

	// TODO: validate agg sig

	senders := make(map[string]struct{}, validator.defaultSize)
	for _, innerMsg := range aggMsg.Messages {
		if !validator.IsSyntacticallyValid(innerMsg) {
			log.Warning("Aggregated validation failed: identified an invalid inner message")
			return false
		}

		// validate unique sender
		pub, err := crypto.NewPublicKey(innerMsg.PubKey)
		if err != nil {
			log.Warning("Aggregated validation failed: could not construct public key: ", err.Error())
			return false
		}
		if _, exist := senders[pub.String()]; exist { // pub already exist
			log.Warning("Aggregated validation failed: detected same pubKey for different messages")
			return false
		}
		senders[pub.String()] = struct{}{} // mark sender as exist

		// validate with attached validators
		for _, validator := range validators {
			if !validator(innerMsg) {
				log.Warning("Aggregated validation failed: attached validator failed")
				return false
			}
		}
	}

	return true
}

func (validator *MessageValidator) validateProposal(msg *pb.HareMessage) bool {
	validateSameIteration := func(m *pb.HareMessage) bool {
		ourIter := iterationFromCounter(msg.Message.K)
		statusIter := iterationFromCounter(m.Message.K)
		if ourIter != statusIter { // not same iteration
			log.Warning("Proposal validation failed: not same iteration. Expected: %v Actual: %v",
				ourIter, statusIter)
			return false
		}

		return true
	}
	validators := []func(m *pb.HareMessage) bool{validateStatusType, validateSameIteration}
	if !validator.validateAggregatedMessage(msg.Message.Svp, validators) {
		log.Warning("Proposal validation failed: failed to validate aggregated message")
		return false
	}

	maxKi := int32(-1) // ki>=-1
	var maxRawSet [][]byte = nil
	for _, status := range msg.Message.Svp.Messages {
		// track max
		if status.Message.Ki > maxKi {
			maxKi = status.Message.Ki
			maxRawSet = status.Message.Values
		}
	}

	if maxKi == -1 { // type A
		if !validateSVPTypeA(msg) {
			log.Warning("Proposal validation failed: type A validation failed")
			return false
		}
	} else {
		if !validator.validateSVPTypeB(msg, maxRawSet) { // type B
			log.Warning("Proposal validation failed: type B validation failed")
			return false
		}
	}

	return true
}

func (validator *MessageValidator) validateCertificate(cert *pb.Certificate) bool {
	if cert == nil {
		log.Warning("Certificate validation failed: certificate is nil")
		return false
	}

	validateSameK := func(m *pb.HareMessage) bool { return m.Message.K == cert.AggMsgs.Messages[0].Message.K }
	validators := []func(m *pb.HareMessage) bool{validateCommitType, validateSameK}
	if !validator.validateAggregatedMessage(cert.AggMsgs, validators) {
		log.Warning("Certificate validation failed: aggregated messages validation failed")
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

// validate SVP for type A (where ki=-1)
func validateSVPTypeA(m *pb.HareMessage) bool {
	s := NewSet(m.Message.Values)
	unionSet := NewEmptySet(cap(m.Message.Values))
	for _, status := range m.Message.Svp.Messages {
		// build union
		for _, buff := range status.Message.Values {
			bid := Value{NewBytes32(buff)}
			unionSet.Add(bid) // assuming add is unique
		}
	}

	if !unionSet.Equals(s) { // s should be the union of all statuses
		log.Warning("Proposal type A validation failed: not a union. Expected: %v Actual: %v", s, unionSet)
		return false
	}

	return true
}

// validate SVP for type B (where ki>=0)
func (validator *MessageValidator) validateSVPTypeB(msg *pb.HareMessage, maxRawSet [][]byte) bool {
	if !validator.validateCertificate(msg.Cert) {
		log.Warning("Proposal validation failed: failed to validate certificate")
		return false
	}

	// max set should be equal to the claimed set
	s := NewSet(msg.Message.Values)
	maxSet := NewSet(maxRawSet)
	if !s.Equals(maxSet) {
		log.Warning("Proposal type B validation failed: max set not equal to proposed set. Expected: %v Actual: %v", s, maxSet)
		return false
	}

	return true
}
