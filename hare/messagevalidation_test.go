package hare

import (
	"github.com/spacemeshos/go-spacemesh/hare/pb"
	"github.com/stretchr/testify/assert"
	"testing"
)

func defaultValidator() *MessageValidator {
	return NewMessageValidator(NewMockSigning(), lowThresh10, lowDefaultSize, func(m *pb.HareMessage) bool {
		return true
	})
}

func TestMessageValidator_CommitStatus(t *testing.T) {
	assert.True(t, validateCommitType(BuildCommitMsg(generateVerifier(t), NewEmptySet(lowDefaultSize))))
	assert.True(t, validateStatusType(BuildStatusMsg(generateVerifier(t), NewEmptySet(lowDefaultSize))))
}

func TestMessageValidator_ValidateCertificate(t *testing.T) {
	validator := defaultValidator()
	assert.False(t, validator.validateCertificate(nil))
	cert := &pb.Certificate{}
	assert.False(t, validator.validateCertificate(cert))
	cert.AggMsgs = &pb.AggregatedMessages{}
	assert.False(t, validator.validateCertificate(cert))
	msgs := make([]*pb.HareMessage, 0, validator.threshold)
	cert.AggMsgs.Messages = msgs
	assert.False(t, validator.validateCertificate(cert))
	msgs = append(msgs, &pb.HareMessage{})
	cert.AggMsgs.Messages = msgs
	assert.False(t, validator.validateCertificate(cert))
	cert.Values = NewSetFromValues(value1).To2DSlice()
	assert.False(t, validator.validateCertificate(cert))

	msgs = make([]*pb.HareMessage, validator.threshold)
	for i := 0; i < validator.threshold; i++ {
		msgs[i] = BuildCommitMsg(generateVerifier(t), NewEmptySet(validator.defaultSize))
	}
	cert.AggMsgs.Messages = msgs
	assert.True(t, validator.validateCertificate(cert))
}

func TestMessageValidator_IsSyntacticallyValid(t *testing.T) {
	validator := defaultValidator()
	assert.False(t, validator.isSyntacticallyValid(nil))
	m := &pb.HareMessage{}
	assert.False(t, validator.isSyntacticallyValid(m))
	m.PubKey = generateVerifier(t).Bytes()
	assert.False(t, validator.isSyntacticallyValid(m))
	m.Message = &pb.InnerMessage{}
	assert.False(t, validator.isSyntacticallyValid(m))
	m.Message.Values = nil
	assert.False(t, validator.isSyntacticallyValid(m))
	m.Message.Values = NewSmallEmptySet().To2DSlice()
	assert.False(t, validator.isSyntacticallyValid(m))
}

func TestMessageValidator_Aggregated(t *testing.T) {
	validator := defaultValidator()
	funcs := make([]func(m *pb.HareMessage)bool, 0)
	assert.False(t, validator.validateAggregatedMessage(nil, funcs))

	agg := &pb.AggregatedMessages{}
	assert.False(t, validator.validateAggregatedMessage(agg, funcs))
	msgs := make([]*pb.HareMessage, validator.threshold)
	for i := 0; i < validator.threshold; i++ {
		msgs[i] = BuildStatusMsg(generateVerifier(t), NewSetFromValues(value1))
	}
	agg.Messages = msgs
	assert.True(t, validator.validateAggregatedMessage(agg, funcs))
	funcs = make([]func(m *pb.HareMessage)bool, 1)
	funcs[0] = func(m *pb.HareMessage) bool {return false}
	assert.False(t, validator.validateAggregatedMessage(agg, funcs))
}

func TestConsensusProcess_isContextuallyValid(t *testing.T) {
	s := NewEmptySet(cfg.SetSize)
	pub := generateVerifier(t)
	cp := generateConsensusProcess(t)

	msgType := make([]MessageType, 4, 4)
	msgType[0] = Status
	msgType[1] = Proposal
	msgType[2] = Commit
	msgType[3] = Notify

	for j := 0; j < len(msgType); j++ {
		for i := 0; i < 4; i++ {
			builder := NewMessageBuilder()
			builder.SetType(msgType[j]).SetInstanceId(*instanceId1).SetRoundCounter(cp.k).SetKi(ki).SetValues(s)
			builder = builder.SetPubKey(pub.Bytes()).Sign(NewMockSigning())
			//mt.Printf("%v   j=%v i=%v Exp: %v Actual %v\n", cp.k, j, i, rounds[j][i], ContextuallyValidateMessage(builder.Build(), cp.k))
			validator := defaultValidator()
			assert.Equal(t, true, validator.ContextuallyValidateMessage(builder.Build(), cp.k))
			cp.advanceToNextRound()
		}
	}
}

func TestMessageValidator_ValidateMessage(t *testing.T) {
	proc := generateConsensusProcess(t)
	v := proc.validator
	assert.True(t, v.ValidateMessage(proc.initDefaultBuilder(proc.s).SetType(PreRound).Sign(proc.signing).Build(), 0))
	assert.True(t, v.ValidateMessage(proc.initDefaultBuilder(proc.s).SetType(Status).Sign(proc.signing).Build(), 0))
}