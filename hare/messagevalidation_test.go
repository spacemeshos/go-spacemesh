package hare

import (
	"github.com/spacemeshos/go-spacemesh/hare/pb"
	"github.com/stretchr/testify/assert"
	"testing"
)

func defaultValidator() *MessageValidator {
	return NewMessageValidator(lowThresh10, lowDefaultSize)
}

func TestMessageValidator_CommitStatus(t *testing.T) {
	assert.True(t, validateCommitType(BuildCommitMsg(generatePubKey(t), NewEmptySet(lowDefaultSize))))
	assert.True(t, validateStatusType(BuildStatusMsg(generatePubKey(t), NewEmptySet(lowDefaultSize))))
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

	msgs = make([]*pb.HareMessage, validator.threshold)
	for i := 0; i < validator.threshold; i++ {
		msgs[i] = BuildCommitMsg(generatePubKey(t), NewEmptySet(validator.defaultSize))
	}
	cert.AggMsgs.Messages = msgs
	assert.True(t, validator.validateCertificate(cert))
}

func TestMessageValidator_IsSyntacticallyValid(t *testing.T) {
	validator := defaultValidator()
	assert.False(t, validator.IsSyntacticallyValid(nil))
	m := &pb.HareMessage{}
	assert.False(t, validator.IsSyntacticallyValid(m))
	m.PubKey = generatePubKey(t).Bytes()
	assert.False(t, validator.IsSyntacticallyValid(m))
	m.Message = &pb.InnerMessage{}
	assert.False(t, validator.IsSyntacticallyValid(m))
	m.Message.Values = NewEmptySet(validator.defaultSize).To2DSlice()
	assert.True(t, validator.IsSyntacticallyValid(m))
}

func TestMessageValidator_Aggregated(t *testing.T) {
	validator := defaultValidator()
	funcs := make([]func(m *pb.HareMessage)bool, 0)
	assert.False(t, validator.validateAggregatedMessage(nil, funcs))

	agg := &pb.AggregatedMessages{}
	assert.False(t, validator.validateAggregatedMessage(agg, funcs))
	msgs := make([]*pb.HareMessage, validator.threshold)
	for i := 0; i < validator.threshold; i++ {
		msgs[i] = BuildStatusMsg(generatePubKey(t), NewEmptySet(validator.defaultSize))
	}
	agg.Messages = msgs
	assert.True(t, validator.validateAggregatedMessage(agg, funcs))
	funcs = make([]func(m *pb.HareMessage)bool, 1)
	funcs[0] = func(m *pb.HareMessage) bool {return false}
	assert.False(t, validator.validateAggregatedMessage(agg, funcs))
}