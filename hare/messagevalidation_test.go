package hare

import (
	"github.com/spacemeshos/go-spacemesh/hare/pb"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/stretchr/testify/assert"
	"testing"
)

func defaultValidator() *syntaxContextValidator {
	return newSyntaxContextValidator(NewMockSigning(), lowThresh10, func(m *pb.HareMessage) bool {
		return true
	}, log.NewDefault("Validator"))
}

func TestMessageValidator_CommitStatus(t *testing.T) {
	assert.True(t, validateCommitType(BuildCommitMsg(generateSigning(t), NewEmptySet(lowDefaultSize))))
	assert.True(t, validateStatusType(BuildStatusMsg(generateSigning(t), NewEmptySet(lowDefaultSize))))
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
		msgs[i] = BuildCommitMsg(generateSigning(t), NewSmallEmptySet())
	}
	cert.AggMsgs.Messages = msgs
	assert.True(t, validator.validateCertificate(cert))
}

func TestEligibilityValidator_validateRole(t *testing.T) {
	oracle := &mockRolacle{}
	ev := NewEligibilityValidator(oracle, log.NewDefault(""))
	ev.oracle = oracle
	assert.False(t, ev.validateRole(nil))
	m := BuildPreRoundMsg(generateSigning(t), NewSmallEmptySet())
	m.Message = nil
	assert.False(t, ev.validateRole(m))
	m = BuildPreRoundMsg(generateSigning(t), NewSmallEmptySet())
	oracle.isEligible = false
	assert.False(t, ev.validateRole(m))
	oracle.isEligible = true
	assert.True(t, ev.validateRole(m))
}

func TestMessageValidator_IsStructureValid(t *testing.T) {
	validator := defaultValidator()
	assert.False(t, validator.SyntacticallyValidateMessage(nil))
	m := &pb.HareMessage{}
	assert.False(t, validator.SyntacticallyValidateMessage(m))
	m.PubKey = generateSigning(t).Verifier().Bytes()
	assert.False(t, validator.SyntacticallyValidateMessage(m))
	m.Message = &pb.InnerMessage{}
	assert.False(t, validator.SyntacticallyValidateMessage(m))
	m.Message.Values = nil
	assert.False(t, validator.SyntacticallyValidateMessage(m))
	m.Message.Values = NewSmallEmptySet().To2DSlice()
	assert.False(t, validator.SyntacticallyValidateMessage(m))
}

func TestMessageValidator_Aggregated(t *testing.T) {
	validator := defaultValidator()
	assert.False(t, validator.validateAggregatedMessage(nil, nil))
	funcs := make([]func(m *pb.HareMessage) bool, 0)
	assert.False(t, validator.validateAggregatedMessage(nil, funcs))

	agg := &pb.AggregatedMessages{}
	assert.False(t, validator.validateAggregatedMessage(agg, funcs))
	msgs := make([]*pb.HareMessage, validator.threshold)
	for i := 0; i < validator.threshold; i++ {
		msgs[i] = BuildStatusMsg(generateSigning(t), NewSetFromValues(value1))
	}
	agg.Messages = msgs
	assert.True(t, validator.validateAggregatedMessage(agg, funcs))
	tmp := msgs[0].PubKey
	msgs[0].PubKey = msgs[1].PubKey
	assert.False(t, validator.validateAggregatedMessage(agg, funcs))
	msgs[0].PubKey = tmp

	funcs = make([]func(m *pb.HareMessage) bool, 1)
	funcs[0] = func(m *pb.HareMessage) bool { return false }
	assert.False(t, validator.validateAggregatedMessage(agg, funcs))
}

func TestConsensusProcess_isContextuallyValid(t *testing.T) {
	s := NewEmptySet(defaultSetSize)
	pub := generateSigning(t)
	cp := generateConsensusProcess(t)

	msgType := make([]MessageType, 4, 4)
	msgType[0] = Status
	msgType[1] = Proposal
	msgType[2] = Commit
	msgType[3] = Notify

	for j := 0; j < len(msgType); j++ {
		for i := 0; i < 4; i++ {
			builder := NewMessageBuilder()
			builder.SetType(msgType[j]).SetInstanceId(instanceId1).SetRoundCounter(cp.k).SetKi(ki).SetValues(s)
			builder = builder.SetPubKey(pub.Verifier().Bytes()).Sign(NewMockSigning())
			//mt.Printf("%v   j=%v i=%v Exp: %v Actual %v\n", cp.k, j, i, rounds[j][i], ContextuallyValidateMessage(builder.Build(), cp.k))
			validator := defaultValidator()
			assert.Equal(t, true, validator.ContextuallyValidateMessage(builder.Build(), cp.k))
			cp.advanceToNextRound()
		}
	}
}

func TestMessageValidator_ValidateMessage(t *testing.T) {
	proc := generateConsensusProcess(t)
	proc.advanceToNextRound()
	v := proc.validator
	preround := proc.initDefaultBuilder(proc.s).SetType(PreRound).Sign(proc.signing).Build()
	assert.True(t, v.SyntacticallyValidateMessage(preround))
	assert.True(t, v.ContextuallyValidateMessage(preround, 0))
	status := proc.initDefaultBuilder(proc.s).SetType(Status).Sign(proc.signing).Build()
	assert.True(t, v.ContextuallyValidateMessage(status, 0))
	assert.True(t, v.SyntacticallyValidateMessage(status))

}

func TestMessageValidator_SyntacticallyValidateMessage(t *testing.T) {
	validator := newSyntaxContextValidator(NewMockSigning(), 1, validate, log.NewDefault("Validator"))
	m := BuildPreRoundMsg(generateSigning(t), NewSmallEmptySet())
	assert.False(t, validator.SyntacticallyValidateMessage(m))
	m = BuildPreRoundMsg(generateSigning(t), NewSetFromValues(value1))
	assert.True(t, validator.SyntacticallyValidateMessage(m))
}

func TestMessageValidator_ContextuallyValidateMessage(t *testing.T) {
	validator := newSyntaxContextValidator(NewMockSigning(), 1, validate, log.NewDefault("Validator"))
	m := BuildPreRoundMsg(generateSigning(t), NewSmallEmptySet())
	m.Message = nil
	assert.False(t, validator.ContextuallyValidateMessage(m, 0))
	m = BuildPreRoundMsg(generateSigning(t), NewSetFromValues(value1))
	assert.True(t, validator.ContextuallyValidateMessage(m, 0))
	m = BuildStatusMsg(generateSigning(t), NewSetFromValues(value1))
	assert.False(t, validator.ContextuallyValidateMessage(m, 1))
	assert.True(t, validator.ContextuallyValidateMessage(m, 0))
}

func TestMessageValidator_validateSVPTypeA(t *testing.T) {
	m := buildProposalMsg(NewMockSigning(), NewSetFromValues(value1, value2, value3), []byte{})
	s1 := NewSetFromValues(value1)
	s2 := NewSetFromValues(value3)
	s3 := NewSetFromValues(value1, value5)
	s4 := NewSetFromValues(value1, value4)
	v := defaultValidator()
	m.Message.Svp = buildSVP(-1, s1, s2, s3, s4)
	assert.False(t, v.validateSVPTypeA(m))
	s3 = NewSetFromValues(value2)
	m.Message.Svp = buildSVP(-1, s1, s2, s3)
	assert.True(t, v.validateSVPTypeA(m))
}

func TestMessageValidator_validateSVPTypeB(t *testing.T) {
	m := buildProposalMsg(NewMockSigning(), NewSetFromValues(value1, value2, value3), []byte{})
	s1 := NewSetFromValues(value1)
	m.Message.Svp = buildSVP(-1, s1)
	s := NewSetFromValues(value1)
	m.Message.Values = s.To2DSlice()
	v := defaultValidator()
	assert.False(t, v.validateSVPTypeB(m, NewSetFromValues(value5)))
	assert.True(t, v.validateSVPTypeB(m, NewSetFromValues(value1)))
}

func TestMessageValidator_validateSVP(t *testing.T) {
	validator := newSyntaxContextValidator(NewMockSigning(), 1, validate, log.NewDefault("Validator"))
	m := buildProposalMsg(NewMockSigning(), NewSetFromValues(value1, value2, value3), []byte{})
	s1 := NewSetFromValues(value1)
	m.Message.Svp = buildSVP(-1, s1)
	m.Message.Svp.Messages[0].Message.Type = int32(Commit)
	assert.False(t, validator.validateSVP(m))
	m.Message.Svp = buildSVP(-1, s1)
	m.Message.Svp.Messages[0].Message.K = 4
	assert.False(t, validator.validateSVP(m))
	m.Message.Svp = buildSVP(-1, s1)
	assert.False(t, validator.validateSVP(m))
	s2 := NewSetFromValues(value1, value2, value3)
	m.Message.Svp = buildSVP(-1, s2)
	assert.True(t, validator.validateSVP(m))
	m.Message.Svp = buildSVP(0, s1)
	assert.False(t, validator.validateSVP(m))
	m.Message.Svp = buildSVP(0, s2)
	assert.True(t, validator.validateSVP(m))
}

func buildSVP(ki int32, S ...*Set) *pb.AggregatedMessages {
	msgs := make([]*pb.HareMessage, 0, len(S))
	for _, s := range S {
		msgs = append(msgs, buildStatusMsg(NewMockSigning(), s, ki))
	}

	svp := &pb.AggregatedMessages{}
	svp.Messages = msgs
	return svp
}
