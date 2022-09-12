package hare

import (
	"context"
	"errors"
	"testing"

	"github.com/golang/mock/gomock"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/hare/mocks"
	"github.com/spacemeshos/go-spacemesh/log/logtest"
	"github.com/spacemeshos/go-spacemesh/signing"
)

type truer struct{}

func (truer) Validate(context.Context, *Msg) bool {
	return true
}

func defaultValidator(tb testing.TB) *syntaxContextValidator {
	trueValidator := func(m *Msg) bool {
		return true
	}
	ctrl := gomock.NewController(tb)
	sq := mocks.NewMockstateQuerier(ctrl)
	sq.EXPECT().IsIdentityActiveOnConsensusView(gomock.Any(), gomock.Any(), gomock.Any()).Return(true, nil).AnyTimes()

	return newSyntaxContextValidator(signing.NewEdSigner(), lowThresh10, trueValidator,
		sq, 10, truer{}, newPubGetter(), logtest.New(tb))
}

func TestMessageValidator_CommitStatus(t *testing.T) {
	assert.True(t, validateCommitType(BuildCommitMsg(signing.NewEdSigner(), NewEmptySet(lowDefaultSize))))
	assert.True(t, validateStatusType(BuildStatusMsg(signing.NewEdSigner(), NewEmptySet(lowDefaultSize))))
}

func TestMessageValidator_ValidateCertificate(t *testing.T) {
	validator := defaultValidator(t)
	assert.False(t, validator.validateCertificate(context.TODO(), nil))
	cert := &Certificate{}
	assert.False(t, validator.validateCertificate(context.TODO(), cert))
	cert.AggMsgs = &AggregatedMessages{}
	assert.False(t, validator.validateCertificate(context.TODO(), cert))
	msgs := make([]Message, 0, validator.threshold)
	cert.AggMsgs.Messages = msgs
	assert.False(t, validator.validateCertificate(context.TODO(), cert))
	msgs = append(msgs, Message{})
	cert.AggMsgs.Messages = msgs
	assert.False(t, validator.validateCertificate(context.TODO(), cert))
	cert.Values = NewSetFromValues(value1).ToSlice()
	assert.False(t, validator.validateCertificate(context.TODO(), cert))

	msgs = make([]Message, validator.threshold)
	for i := 0; i < validator.threshold; i++ {
		msgs[i] = BuildCommitMsg(signing.NewEdSigner(), NewDefaultEmptySet()).Message
	}
	cert.AggMsgs.Messages = msgs
	assert.True(t, validator.validateCertificate(context.TODO(), cert))
}

func TestEligibilityValidator_validateRole_NoMsg(t *testing.T) {
	types.SetLayersPerEpoch(10)
	ev := newEligibilityValidator(nil, 10, 1, 5, logtest.New(t))
	res, err := ev.validateRole(context.TODO(), nil)
	assert.NotNil(t, err)
	assert.False(t, res)
}

func TestEligibilityValidator_validateRole_NoInnerMsg(t *testing.T) {
	types.SetLayersPerEpoch(10)
	ev := newEligibilityValidator(nil, 10, 1, 5, logtest.New(t))
	m := BuildPreRoundMsg(signing.NewEdSigner(), NewDefaultEmptySet(), nil)
	m.InnerMsg = nil
	res, err := ev.validateRole(context.TODO(), m)
	assert.NotNil(t, err)
	assert.False(t, res)
}

func TestEligibilityValidator_validateRole_Genesis(t *testing.T) {
	types.SetLayersPerEpoch(10)
	ev := newEligibilityValidator(nil, 10, 1, 5, logtest.New(t))
	m := BuildPreRoundMsg(signing.NewEdSigner(), NewDefaultEmptySet(), nil)
	res, err := ev.validateRole(context.TODO(), m)
	assert.Nil(t, err)
	// TODO: remove comment after inceptions problem is addressed
	// assert.False(t, res)
	assert.True(t, res)
}

func TestEligibilityValidator_validateRole_FailedToValidate(t *testing.T) {
	types.SetLayersPerEpoch(10)
	ctrl := gomock.NewController(t)
	mo := mocks.NewMockRolacle(ctrl)
	ev := newEligibilityValidator(mo, 10, 1, 5, logtest.New(t))
	m := BuildPreRoundMsg(signing.NewEdSigner(), NewDefaultEmptySet(), nil)
	m.InnerMsg.InstanceID = types.NewLayerID(111)
	myErr := errors.New("my error")

	mo.EXPECT().Validate(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return(false, myErr).Times(1)
	res, err := ev.validateRole(context.TODO(), m)
	assert.NotNil(t, err)
	assert.ErrorIs(t, err, myErr)
	assert.False(t, res)
}

func TestEligibilityValidator_validateRole_NotEligible(t *testing.T) {
	types.SetLayersPerEpoch(10)
	ctrl := gomock.NewController(t)
	mo := mocks.NewMockRolacle(ctrl)
	ev := newEligibilityValidator(mo, 10, 1, 5, logtest.New(t))
	m := BuildPreRoundMsg(signing.NewEdSigner(), NewDefaultEmptySet(), nil)
	m.InnerMsg.InstanceID = types.NewLayerID(111)

	mo.EXPECT().Validate(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return(false, nil).Times(1)
	res, err := ev.validateRole(context.TODO(), m)
	assert.Nil(t, err)
	assert.False(t, res)
}

func TestEligibilityValidator_validateRole_Success(t *testing.T) {
	types.SetLayersPerEpoch(10)
	ctrl := gomock.NewController(t)
	mo := mocks.NewMockRolacle(ctrl)
	ev := newEligibilityValidator(mo, 10, 1, 5, logtest.New(t))
	m := BuildPreRoundMsg(signing.NewEdSigner(), NewDefaultEmptySet(), nil)
	m.InnerMsg.InstanceID = types.NewLayerID(111)

	mo.EXPECT().Validate(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return(true, nil).Times(1)
	res, err := ev.validateRole(context.TODO(), m)
	assert.Nil(t, err)
	assert.True(t, res)
}

func TestMessageValidator_IsStructureValid(t *testing.T) {
	validator := defaultValidator(t)
	assert.False(t, validator.SyntacticallyValidateMessage(context.TODO(), nil))
	m := &Msg{Message: Message{}, PubKey: nil}
	assert.False(t, validator.SyntacticallyValidateMessage(context.TODO(), m))
	m.PubKey = signing.NewEdSigner().PublicKey()
	assert.False(t, validator.SyntacticallyValidateMessage(context.TODO(), m))

	// empty set is allowed now
	m.InnerMsg = &InnerMessage{}
	assert.True(t, validator.SyntacticallyValidateMessage(context.TODO(), m))
	m.InnerMsg.Values = nil
	assert.True(t, validator.SyntacticallyValidateMessage(context.TODO(), m))
	m.InnerMsg.Values = NewDefaultEmptySet().ToSlice()
	assert.True(t, validator.SyntacticallyValidateMessage(context.TODO(), m))
}

type mockValidator struct {
	res bool
}

func (m mockValidator) Validate(context.Context, *Msg) bool {
	return m.res
}

func initPg(validator *syntaxContextValidator) (*pubGetter, []Message, Signer) {
	pg := newPubGetter()
	msgs := make([]Message, validator.threshold)
	sgn := signing.NewEdSigner()
	for i := 0; i < validator.threshold; i++ {
		sgn = signing.NewEdSigner() // hold some sgn
		iMsg := BuildStatusMsg(sgn, NewSetFromValues(value1))
		msgs[i] = iMsg.Message
		pg.Track(iMsg)
	}

	return pg, msgs, sgn
}

func TestMessageValidator_Aggregated(t *testing.T) {
	r := require.New(t)
	validator := defaultValidator(t)
	r.Equal(errNilValidators, validator.validateAggregatedMessage(context.TODO(), nil, nil))
	funcs := make([]func(m *Msg) bool, 0)
	r.Equal(errNilAggMsgs, validator.validateAggregatedMessage(context.TODO(), nil, funcs))

	agg := &AggregatedMessages{}
	r.Equal(errNilMsgsSlice, validator.validateAggregatedMessage(context.TODO(), agg, funcs))

	agg.Messages = makeMessages(validator.threshold - 1)
	r.Equal(errMsgsCountMismatch, validator.validateAggregatedMessage(context.TODO(), agg, funcs))

	pg, msgs, sgn := initPg(validator)

	validator.validMsgsTracker = pg
	agg.Messages = msgs
	r.Nil(validator.validateAggregatedMessage(context.TODO(), agg, funcs))

	validator.validMsgsTracker = newPubGetter()
	tmp := msgs[0].Sig
	msgs[0].Sig = []byte{1}
	r.Error(validator.validateAggregatedMessage(context.TODO(), agg, funcs))

	msgs[0].Sig = tmp
	inner := msgs[0].InnerMsg.Values
	msgs[0].InnerMsg.Values = inner
	validator.roleValidator = &mockValidator{}
	r.Equal(errInnerEligibility, validator.validateAggregatedMessage(context.TODO(), agg, funcs))

	validator.roleValidator = &mockValidator{true}
	funcs = make([]func(m *Msg) bool, 1)
	funcs[0] = func(m *Msg) bool { return false }
	r.Equal(errInnerFunc, validator.validateAggregatedMessage(context.TODO(), agg, funcs))

	funcs[0] = func(m *Msg) bool { return true }
	m0 := msgs[0]
	msgs[0] = BuildStatusMsg(sgn, NewSetFromValues(value1)).Message
	r.Equal(errDupSender, validator.validateAggregatedMessage(context.TODO(), agg, funcs))

	validator.validMsgsTracker = pg
	msgs[0] = m0
	msgs[len(msgs)-1] = m0
	r.Equal(errDupSender, validator.validateAggregatedMessage(context.TODO(), agg, funcs))

	msgs[0] = BuildStatusMsg(signing.NewEdSigner(), NewSetFromValues(value1)).Message
	r.Nil(pg.PublicKey(&msgs[0]))
	require.NoError(t, validator.validateAggregatedMessage(context.TODO(), agg, funcs))
	r.NotNil(pg.PublicKey(&msgs[0]))
}

func makeMessages(eligibilityCount int) []Message {
	return []Message{
		{
			InnerMsg: &InnerMessage{
				EligibilityCount: uint16(eligibilityCount),
			},
		},
	}
}

func TestSyntaxContextValidator_PreRoundContext(t *testing.T) {
	r := require.New(t)
	validator := defaultValidator(t)
	ed := signing.NewEdSigner()
	pre := BuildPreRoundMsg(ed, NewDefaultEmptySet(), nil)
	for i := uint32(0); i < 10; i++ {
		k := i * 4
		pre.InnerMsg.K = k
		e := validator.ContextuallyValidateMessage(context.TODO(), pre, k)
		r.Nil(e)
	}
}

func TestSyntaxContextValidator_ContextuallyValidateMessageForIteration(t *testing.T) {
	r := require.New(t)
	v := defaultValidator(t)
	ed := signing.NewEdSigner()
	set := NewDefaultEmptySet()
	pre := BuildPreRoundMsg(ed, set, nil)
	pre.InnerMsg.K = preRound
	r.Nil(v.ContextuallyValidateMessage(context.TODO(), pre, 1))

	status := BuildStatusMsg(ed, set)
	status.InnerMsg.K = 100
	r.Equal(errInvalidIter, v.ContextuallyValidateMessage(context.TODO(), status, 1))

	proposal := BuildCommitMsg(ed, set)
	proposal.InnerMsg.K = 100
	r.Equal(errInvalidIter, v.ContextuallyValidateMessage(context.TODO(), proposal, 1))

	commit := BuildCommitMsg(ed, set)
	commit.InnerMsg.K = 100
	r.Equal(errInvalidIter, v.ContextuallyValidateMessage(context.TODO(), commit, 1))

	notify := BuildNotifyMsg(ed, set)
	notify.InnerMsg.K = 100
	r.Equal(errInvalidIter, v.ContextuallyValidateMessage(context.TODO(), notify, 1))
}

func TestMessageValidator_ValidateMessage(t *testing.T) {
	proc := generateConsensusProcess(t)
	proc.advanceToNextRound(context.TODO())
	v := proc.validator
	b, err := proc.initDefaultBuilder(proc.s)
	assert.Nil(t, err)
	preround := b.SetType(pre).Sign(proc.signing).Build()
	preround.PubKey = proc.signing.PublicKey()
	assert.True(t, v.SyntacticallyValidateMessage(context.TODO(), preround))
	e := v.ContextuallyValidateMessage(context.TODO(), preround, 0)
	assert.Nil(t, e)
	b, err = proc.initDefaultBuilder(proc.s)
	assert.Nil(t, err)
	status := b.SetType(status).Sign(proc.signing).Build()
	status.PubKey = proc.signing.PublicKey()
	e = v.ContextuallyValidateMessage(context.TODO(), status, 0)
	assert.Nil(t, e)
	assert.True(t, v.SyntacticallyValidateMessage(context.TODO(), status))
}

type pubGetter struct {
	mp map[string]*signing.PublicKey
}

func newPubGetter() *pubGetter {
	return &pubGetter{make(map[string]*signing.PublicKey)}
}

func (pg pubGetter) Track(m *Msg) {
	pg.mp[string(m.Sig)] = m.PubKey
}

func (pg pubGetter) PublicKey(m *Message) *signing.PublicKey {
	if pg.mp == nil {
		return nil
	}

	p, ok := pg.mp[string(m.Sig)]
	if !ok {
		return nil
	}

	return p
}

func TestMessageValidator_SyntacticallyValidateMessage(t *testing.T) {
	validator := newSyntaxContextValidator(signing.NewEdSigner(), 1, validate, nil, 10, truer{}, newPubGetter(), logtest.New(t))
	m := BuildPreRoundMsg(signing.NewEdSigner(), NewDefaultEmptySet(), nil)
	assert.True(t, validator.SyntacticallyValidateMessage(context.TODO(), m))
	m = BuildPreRoundMsg(signing.NewEdSigner(), NewSetFromValues(value1), nil)
	assert.True(t, validator.SyntacticallyValidateMessage(context.TODO(), m))
}

func TestMessageValidator_validateSVPTypeA(t *testing.T) {
	m := buildProposalMsg(signing.NewEdSigner(), NewSetFromValues(value1, value2, value3), []byte{})
	s1 := NewSetFromValues(value1)
	s2 := NewSetFromValues(value3)
	s3 := NewSetFromValues(value1, value5)
	s4 := NewSetFromValues(value1, value4)
	v := defaultValidator(t)
	m.InnerMsg.Svp = buildSVP(preRound, s1, s2, s3, s4)
	assert.False(t, v.validateSVPTypeA(context.TODO(), m))
	s3 = NewSetFromValues(value2)
	m.InnerMsg.Svp = buildSVP(preRound, s1, s2, s3)
	assert.True(t, v.validateSVPTypeA(context.TODO(), m))
}

func TestMessageValidator_validateSVPTypeB(t *testing.T) {
	m := buildProposalMsg(signing.NewEdSigner(), NewSetFromValues(value1, value2, value3), []byte{})
	s1 := NewSetFromValues(value1)
	m.InnerMsg.Svp = buildSVP(preRound, s1)
	s := NewSetFromValues(value1)
	m.InnerMsg.Values = s.ToSlice()
	v := defaultValidator(t)
	assert.False(t, v.validateSVPTypeB(context.TODO(), m, NewSetFromValues(value5)))
	assert.True(t, v.validateSVPTypeB(context.TODO(), m, NewSetFromValues(value1)))
}

func TestMessageValidator_validateSVP(t *testing.T) {
	ctrl := gomock.NewController(t)
	mockStateQ := mocks.NewMockstateQuerier(ctrl)
	mockStateQ.EXPECT().IsIdentityActiveOnConsensusView(gomock.Any(), gomock.Any(), gomock.Any()).Return(true, nil).AnyTimes()
	validator := newSyntaxContextValidator(signing.NewEdSigner(), 1, validate, mockStateQ, 10, truer{}, newPubGetter(), logtest.New(t))
	m := buildProposalMsg(signing.NewEdSigner(), NewSetFromValues(value1, value2, value3), []byte{})
	s1 := NewSetFromValues(value1)
	m.InnerMsg.Svp = buildSVP(preRound, s1)
	m.InnerMsg.Svp.Messages[0].InnerMsg.Type = commit
	assert.False(t, validator.validateSVP(context.TODO(), m))
	m.InnerMsg.Svp = buildSVP(preRound, s1)
	m.InnerMsg.Svp.Messages[0].InnerMsg.K = 4
	assert.False(t, validator.validateSVP(context.TODO(), m))
	m.InnerMsg.Svp = buildSVP(preRound, s1)
	assert.False(t, validator.validateSVP(context.TODO(), m))
	s2 := NewSetFromValues(value1, value2, value3)
	m.InnerMsg.Svp = buildSVP(preRound, s2)
	assert.True(t, validator.validateSVP(context.TODO(), m))
	m.InnerMsg.Svp = buildSVP(0, s1)
	assert.False(t, validator.validateSVP(context.TODO(), m))
	m.InnerMsg.Svp = buildSVP(0, s2)
	assert.True(t, validator.validateSVP(context.TODO(), m))
}

func buildSVP(ki uint32, S ...*Set) *AggregatedMessages {
	msgs := make([]Message, 0, len(S))
	for _, s := range S {
		msgs = append(msgs, buildStatusMsg(signing.NewEdSigner(), s, ki).Message)
	}

	svp := &AggregatedMessages{}
	svp.Messages = msgs
	return svp
}

func validateMatrix(t *testing.T, mType MessageType, msgK uint32, exp []error) {
	r := require.New(t)
	rounds := []uint32{preRound, 0, 1, 2, 3, 4, 5, 6, 7}
	v := defaultValidator(t)
	sgn := signing.NewEdSigner()
	set := NewEmptySet(1)
	var m *Msg
	switch mType {
	case status:
		m = BuildStatusMsg(sgn, set)
	case proposal:
		m = BuildProposalMsg(sgn, set)
	case commit:
		m = BuildCommitMsg(sgn, set)
	case notify:
		m = BuildNotifyMsg(sgn, set)
	default:
		panic("unexpected msg type")
	}

	for i, round := range rounds {
		m.InnerMsg.K = msgK
		e := v.ContextuallyValidateMessage(context.TODO(), m, round)
		r.Equal(exp[i], e, "msgK=%v\tround=%v\texp=%v\tactual=%v", msgK, round, exp[i], e)
	}
}

func TestSyntaxContextValidator_StatusContextMatrix(t *testing.T) {
	msg0 := []error{errEarlyMsg, nil, errInvalidRound, errInvalidRound, errInvalidRound, errInvalidIter, errInvalidIter, errInvalidIter, errInvalidIter}
	msg4 := []error{errInvalidIter, errInvalidIter, errInvalidIter, errInvalidIter, errEarlyMsg, nil, errInvalidRound, errInvalidRound, errInvalidRound}
	validateMatrix(t, status, 0, msg0)
	validateMatrix(t, status, 4, msg4)
}

func TestSyntaxContextValidator_ProposalContextMatrix(t *testing.T) {
	msg1 := []error{errInvalidRound, errEarlyMsg, nil, nil, errInvalidRound, errInvalidIter, errInvalidIter, errInvalidIter, errInvalidIter}
	msg5 := []error{errInvalidIter, errInvalidIter, errInvalidIter, errInvalidIter, errInvalidIter, errEarlyMsg, nil, nil, errInvalidRound}
	validateMatrix(t, proposal, 1, msg1)
	validateMatrix(t, proposal, 5, msg5)
}

func TestSyntaxContextValidator_CommitContextMatrix(t *testing.T) {
	msg2 := []error{errInvalidRound, errInvalidRound, errEarlyMsg, nil, errInvalidRound, errInvalidIter, errInvalidIter, errInvalidIter, errInvalidIter}
	msg6 := []error{errInvalidIter, errInvalidIter, errInvalidIter, errInvalidIter, errInvalidIter, errInvalidRound, errEarlyMsg, nil, errInvalidRound}
	validateMatrix(t, commit, 2, msg2)
	validateMatrix(t, commit, 6, msg6)
}

func TestSyntaxContextValidator_NotifyContextMatrix(t *testing.T) {
	msg3 := []error{errInvalidRound, errInvalidRound, errInvalidRound, errEarlyMsg, nil, nil, nil, nil, nil}
	msg7 := []error{errInvalidIter, errInvalidIter, errInvalidIter, errInvalidIter, errInvalidIter, errInvalidRound, errInvalidRound, errEarlyMsg, nil}
	validateMatrix(t, notify, 3, msg3)
	validateMatrix(t, notify, 7, msg7)
}
