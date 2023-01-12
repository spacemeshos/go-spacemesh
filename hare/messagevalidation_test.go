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

	signer, err := signing.NewEdSigner()
	require.NoError(tb, err)

	return newSyntaxContextValidator(signer, lowThresh10, trueValidator,
		sq, 10, truer{}, newPubGetter(), logtest.New(tb))
}

func TestMessageValidator_CommitStatus(t *testing.T) {
	signer1, err := signing.NewEdSigner()
	require.NoError(t, err)
	signer2, err := signing.NewEdSigner()
	require.NoError(t, err)

	assert.True(t, validateCommitType(BuildCommitMsg(signer1, NewEmptySet(lowDefaultSize))))
	assert.True(t, validateStatusType(BuildStatusMsg(signer2, NewEmptySet(lowDefaultSize))))
}

func TestMessageValidator_ValidateCertificate(t *testing.T) {
	validator := defaultValidator(t)
	assert.False(t, validator.validateCertificate(context.Background(), nil))
	cert := &Certificate{}
	assert.False(t, validator.validateCertificate(context.Background(), cert))
	cert.AggMsgs = &AggregatedMessages{}
	assert.False(t, validator.validateCertificate(context.Background(), cert))
	msgs := make([]Message, 0, validator.threshold)
	cert.AggMsgs.Messages = msgs
	assert.False(t, validator.validateCertificate(context.Background(), cert))
	msgs = append(msgs, Message{})
	cert.AggMsgs.Messages = msgs
	assert.False(t, validator.validateCertificate(context.Background(), cert))
	cert.Values = NewSetFromValues(value1).ToSlice()
	assert.False(t, validator.validateCertificate(context.Background(), cert))

	msgs = make([]Message, validator.threshold)
	for i := 0; i < validator.threshold; i++ {
		signer, err := signing.NewEdSigner()
		require.NoError(t, err)
		msgs[i] = BuildCommitMsg(signer, NewDefaultEmptySet()).Message
	}
	cert.AggMsgs.Messages = msgs
	assert.True(t, validator.validateCertificate(context.Background(), cert))
}

func TestEligibilityValidator_validateRole_NoMsg(t *testing.T) {
	ev := newEligibilityValidator(nil, 4, 1, 5, logtest.New(t))
	res, err := ev.validateRole(context.Background(), nil)
	assert.NotNil(t, err)
	assert.False(t, res)
}

func TestEligibilityValidator_validateRole_NoInnerMsg(t *testing.T) {
	ev := newEligibilityValidator(nil, 4, 1, 5, logtest.New(t))
	signer, err := signing.NewEdSigner()
	require.NoError(t, err)

	m := BuildPreRoundMsg(signer, NewDefaultEmptySet(), nil)
	m.InnerMsg = nil
	res, err := ev.validateRole(context.Background(), m)
	assert.NotNil(t, err)
	assert.False(t, res)
}

func TestEligibilityValidator_validateRole_Genesis(t *testing.T) {
	ev := newEligibilityValidator(nil, 4, 1, 5, logtest.New(t))
	sig, err := signing.NewEdSigner()
	require.NoError(t, err)
	builder := newMessageBuilder().
		SetType(pre).
		SetLayer(types.NewLayerID(1)).
		SetRoundCounter(k).
		SetCommittedRound(ki).
		SetValues(NewDefaultEmptySet()).
		SetPubKey(sig.PublicKey()).
		SetEligibilityCount(1)
	builder.Sign(sig)
	m := builder.Build()

	res, err := ev.validateRole(context.Background(), m)
	assert.NoError(t, err)
	// TODO: remove comment after inceptions problem is addressed
	// assert.False(t, res)
	assert.True(t, res)
}

func TestEligibilityValidator_validateRole_FailedToValidate(t *testing.T) {
	ctrl := gomock.NewController(t)
	mo := mocks.NewMockRolacle(ctrl)
	ev := newEligibilityValidator(mo, 4, 1, 5, logtest.New(t))

	signer, err := signing.NewEdSigner()
	require.NoError(t, err)

	m := BuildPreRoundMsg(signer, NewDefaultEmptySet(), nil)
	m.Layer = types.NewLayerID(111)
	myErr := errors.New("my error")

	mo.EXPECT().Validate(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return(false, myErr).Times(1)
	res, err := ev.validateRole(context.Background(), m)
	assert.NotNil(t, err)
	assert.ErrorIs(t, err, myErr)
	assert.False(t, res)
}

func TestEligibilityValidator_validateRole_NotEligible(t *testing.T) {
	ctrl := gomock.NewController(t)
	mo := mocks.NewMockRolacle(ctrl)
	ev := newEligibilityValidator(mo, 4, 1, 5, logtest.New(t))

	signer, err := signing.NewEdSigner()
	require.NoError(t, err)

	m := BuildPreRoundMsg(signer, NewDefaultEmptySet(), nil)
	m.Layer = types.NewLayerID(111)

	mo.EXPECT().Validate(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return(false, nil).Times(1)
	res, err := ev.validateRole(context.Background(), m)
	assert.Nil(t, err)
	assert.False(t, res)
}

func TestEligibilityValidator_validateRole_Success(t *testing.T) {
	ctrl := gomock.NewController(t)
	mo := mocks.NewMockRolacle(ctrl)
	ev := newEligibilityValidator(mo, 4, 1, 5, logtest.New(t))

	signer, err := signing.NewEdSigner()
	require.NoError(t, err)

	m := BuildPreRoundMsg(signer, NewDefaultEmptySet(), nil)
	m.Layer = types.NewLayerID(111)

	mo.EXPECT().Validate(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return(true, nil).Times(1)
	res, err := ev.validateRole(context.Background(), m)
	assert.Nil(t, err)
	assert.True(t, res)
}

func TestMessageValidator_IsStructureValid(t *testing.T) {
	validator := defaultValidator(t)

	signer, err := signing.NewEdSigner()
	require.NoError(t, err)

	assert.False(t, validator.SyntacticallyValidateMessage(context.Background(), nil))
	m := &Msg{Message: Message{}, PubKey: nil}
	assert.False(t, validator.SyntacticallyValidateMessage(context.Background(), m))
	m.PubKey = signer.PublicKey()
	assert.False(t, validator.SyntacticallyValidateMessage(context.Background(), m))

	// empty set is allowed now
	m.InnerMsg = &InnerMessage{}
	assert.True(t, validator.SyntacticallyValidateMessage(context.Background(), m))
	m.InnerMsg.Values = nil
	assert.True(t, validator.SyntacticallyValidateMessage(context.Background(), m))
	m.InnerMsg.Values = NewDefaultEmptySet().ToSlice()
	assert.True(t, validator.SyntacticallyValidateMessage(context.Background(), m))
}

type mockValidator struct {
	res bool
}

func (m mockValidator) Validate(context.Context, *Msg) bool {
	return m.res
}

func initPg(tb testing.TB, validator *syntaxContextValidator) (*pubGetter, []Message, Signer) {
	pg := newPubGetter()
	msgs := make([]Message, validator.threshold)
	var signer *signing.EdSigner
	for i := 0; i < validator.threshold; i++ {
		var err error
		signer, err = signing.NewEdSigner()
		require.NoError(tb, err)
		iMsg := BuildStatusMsg(signer, NewSetFromValues(value1))
		msgs[i] = iMsg.Message
		pg.Track(iMsg)
	}

	return pg, msgs, signer
}

func TestMessageValidator_Aggregated(t *testing.T) {
	r := require.New(t)
	validator := defaultValidator(t)
	r.Equal(errNilValidators, validator.validateAggregatedMessage(context.Background(), nil, nil))
	funcs := make([]func(m *Msg) bool, 0)
	r.Equal(errNilAggMsgs, validator.validateAggregatedMessage(context.Background(), nil, funcs))

	agg := &AggregatedMessages{}
	r.Equal(errNilMsgsSlice, validator.validateAggregatedMessage(context.Background(), agg, funcs))

	agg.Messages = makeMessages(validator.threshold - 1)
	r.ErrorIs(validator.validateAggregatedMessage(context.Background(), agg, funcs), errMsgsCountMismatch)

	pg, msgs, sgn := initPg(t, validator)

	validator.validMsgsTracker = pg
	agg.Messages = msgs
	r.Nil(validator.validateAggregatedMessage(context.Background(), agg, funcs))

	validator.validMsgsTracker = newPubGetter()
	tmp := msgs[0].Signature
	msgs[0].Signature = []byte{1}
	r.Error(validator.validateAggregatedMessage(context.Background(), agg, funcs))

	msgs[0].Signature = tmp
	inner := msgs[0].InnerMsg.Values
	msgs[0].InnerMsg.Values = inner
	validator.roleValidator = &mockValidator{}
	r.Equal(errInnerEligibility, validator.validateAggregatedMessage(context.Background(), agg, funcs))

	validator.roleValidator = &mockValidator{true}
	funcs = make([]func(m *Msg) bool, 1)
	funcs[0] = func(m *Msg) bool { return false }
	r.Equal(errInnerFunc, validator.validateAggregatedMessage(context.Background(), agg, funcs))

	funcs[0] = func(m *Msg) bool { return true }
	m0 := msgs[0]
	msgs[0] = BuildStatusMsg(sgn, NewSetFromValues(value1)).Message
	r.Equal(errDupSender, validator.validateAggregatedMessage(context.Background(), agg, funcs))

	validator.validMsgsTracker = pg
	msgs[0] = m0
	msgs[len(msgs)-1] = m0
	r.Equal(errDupSender, validator.validateAggregatedMessage(context.Background(), agg, funcs))

	signer, err := signing.NewEdSigner()
	require.NoError(t, err)

	msgs[0] = BuildStatusMsg(signer, NewSetFromValues(value1)).Message
	r.Nil(pg.PublicKey(&msgs[0]))
	require.NoError(t, validator.validateAggregatedMessage(context.Background(), agg, funcs))
	r.NotNil(pg.PublicKey(&msgs[0]))
}

func makeMessages(eligibilityCount int) []Message {
	return []Message{
		{
			Eligibility: types.HareEligibility{
				Count: uint16(eligibilityCount),
			},
		},
	}
}

func TestSyntaxContextValidator_PreRoundContext(t *testing.T) {
	r := require.New(t)
	validator := defaultValidator(t)
	signer, err := signing.NewEdSigner()
	require.NoError(t, err)
	pre := BuildPreRoundMsg(signer, NewDefaultEmptySet(), nil)
	for i := uint32(0); i < 10; i++ {
		k := i * 4
		pre.Round = k
		e := validator.ContextuallyValidateMessage(context.Background(), pre, k)
		r.Nil(e)
	}
}

func TestSyntaxContextValidator_ContextuallyValidateMessageForIteration(t *testing.T) {
	r := require.New(t)
	v := defaultValidator(t)
	signer, err := signing.NewEdSigner()
	require.NoError(t, err)
	set := NewDefaultEmptySet()
	pre := BuildPreRoundMsg(signer, set, nil)
	pre.Round = preRound
	r.Nil(v.ContextuallyValidateMessage(context.Background(), pre, 1))

	status := BuildStatusMsg(signer, set)
	status.Round = 100
	r.Equal(errInvalidIter, v.ContextuallyValidateMessage(context.Background(), status, 1))

	proposal := BuildCommitMsg(signer, set)
	proposal.Round = 100
	r.Equal(errInvalidIter, v.ContextuallyValidateMessage(context.Background(), proposal, 1))

	commit := BuildCommitMsg(signer, set)
	commit.Round = 100
	r.Equal(errInvalidIter, v.ContextuallyValidateMessage(context.Background(), commit, 1))

	notify := BuildNotifyMsg(signer, set)
	notify.Round = 100
	r.Equal(errInvalidIter, v.ContextuallyValidateMessage(context.Background(), notify, 1))
}

func TestMessageValidator_ValidateMessage(t *testing.T) {
	proc := generateConsensusProcess(t)
	proc.advanceToNextRound(context.Background())
	v := proc.validator
	b, err := proc.initDefaultBuilder(proc.value)
	assert.Nil(t, err)
	preround := b.SetType(pre).Sign(proc.signing).Build()
	preround.PubKey = proc.signing.PublicKey()
	assert.True(t, v.SyntacticallyValidateMessage(context.Background(), preround))
	e := v.ContextuallyValidateMessage(context.Background(), preround, 0)
	assert.Nil(t, e)
	b, err = proc.initDefaultBuilder(proc.value)
	assert.Nil(t, err)
	status := b.SetType(status).Sign(proc.signing).Build()
	status.PubKey = proc.signing.PublicKey()
	e = v.ContextuallyValidateMessage(context.Background(), status, 0)
	assert.Nil(t, e)
	assert.True(t, v.SyntacticallyValidateMessage(context.Background(), status))
}

type pubGetter struct {
	mp map[string]*signing.PublicKey
}

func newPubGetter() *pubGetter {
	return &pubGetter{make(map[string]*signing.PublicKey)}
}

func (pg pubGetter) Track(m *Msg) {
	pg.mp[string(m.Signature)] = m.PubKey
}

func (pg pubGetter) PublicKey(m *Message) *signing.PublicKey {
	if pg.mp == nil {
		return nil
	}

	p, ok := pg.mp[string(m.Signature)]
	if !ok {
		return nil
	}

	return p
}

func TestMessageValidator_SyntacticallyValidateMessage(t *testing.T) {
	signer, err := signing.NewEdSigner()
	require.NoError(t, err)

	validator := newSyntaxContextValidator(signer, 1, validate, nil, 10, truer{}, newPubGetter(), logtest.New(t))
	m := BuildPreRoundMsg(signer, NewDefaultEmptySet(), nil)
	assert.True(t, validator.SyntacticallyValidateMessage(context.Background(), m))
	m = BuildPreRoundMsg(signer, NewSetFromValues(value1), nil)
	assert.True(t, validator.SyntacticallyValidateMessage(context.Background(), m))
}

func TestMessageValidator_validateSVPTypeA(t *testing.T) {
	signer, err := signing.NewEdSigner()
	require.NoError(t, err)

	m := buildProposalMsg(signer, NewSetFromValues(value1, value2, value3), []byte{})
	s1 := NewSetFromValues(value1)
	s2 := NewSetFromValues(value3)
	s3 := NewSetFromValues(value1, value5)
	s4 := NewSetFromValues(value1, value4)
	v := defaultValidator(t)
	m.InnerMsg.Svp = buildSVP(preRound, s1, s2, s3, s4)
	assert.False(t, v.validateSVPTypeA(context.Background(), m))
	s3 = NewSetFromValues(value2)
	m.InnerMsg.Svp = buildSVP(preRound, s1, s2, s3)
	assert.True(t, v.validateSVPTypeA(context.Background(), m))
}

func TestMessageValidator_validateSVPTypeB(t *testing.T) {
	signer, err := signing.NewEdSigner()
	require.NoError(t, err)

	m := buildProposalMsg(signer, NewSetFromValues(value1, value2, value3), []byte{})
	s1 := NewSetFromValues(value1)
	m.InnerMsg.Svp = buildSVP(preRound, s1)
	s := NewSetFromValues(value1)
	m.InnerMsg.Values = s.ToSlice()
	v := defaultValidator(t)
	assert.False(t, v.validateSVPTypeB(context.Background(), m, NewSetFromValues(value5)))
	assert.True(t, v.validateSVPTypeB(context.Background(), m, NewSetFromValues(value1)))
}

func TestMessageValidator_validateSVP(t *testing.T) {
	signer, err := signing.NewEdSigner()
	require.NoError(t, err)

	ctrl := gomock.NewController(t)
	mockStateQ := mocks.NewMockstateQuerier(ctrl)
	mockStateQ.EXPECT().IsIdentityActiveOnConsensusView(gomock.Any(), gomock.Any(), gomock.Any()).Return(true, nil).AnyTimes()
	validator := newSyntaxContextValidator(signer, 1, validate, mockStateQ, 10, truer{}, newPubGetter(), logtest.New(t))
	m := buildProposalMsg(signer, NewSetFromValues(value1, value2, value3), []byte{})
	s1 := NewSetFromValues(value1)
	m.InnerMsg.Svp = buildSVP(preRound, s1)
	m.InnerMsg.Svp.Messages[0].InnerMsg.Type = commit
	assert.False(t, validator.validateSVP(context.Background(), m))
	m.InnerMsg.Svp = buildSVP(preRound, s1)
	m.InnerMsg.Svp.Messages[0].Round = 4
	assert.False(t, validator.validateSVP(context.Background(), m))
	m.InnerMsg.Svp = buildSVP(preRound, s1)
	assert.False(t, validator.validateSVP(context.Background(), m))
	s2 := NewSetFromValues(value1, value2, value3)
	m.InnerMsg.Svp = buildSVP(preRound, s2)
	assert.True(t, validator.validateSVP(context.Background(), m))
	m.InnerMsg.Svp = buildSVP(0, s1)
	assert.False(t, validator.validateSVP(context.Background(), m))
	m.InnerMsg.Svp = buildSVP(0, s2)
	assert.True(t, validator.validateSVP(context.Background(), m))
}

func buildSVP(ki uint32, S ...*Set) *AggregatedMessages {
	msgs := make([]Message, 0, len(S))
	for _, s := range S {
		signer, _ := signing.NewEdSigner()
		msgs = append(msgs, buildStatusMsg(signer, s, ki).Message)
	}

	svp := &AggregatedMessages{}
	svp.Messages = msgs
	return svp
}

func validateMatrix(tb testing.TB, mType MessageType, msgK uint32, exp []error) {
	r := require.New(tb)
	rounds := []uint32{preRound, 0, 1, 2, 3, 4, 5, 6, 7}
	v := defaultValidator(tb)
	signer, err := signing.NewEdSigner()
	require.NoError(tb, err)
	set := NewEmptySet(1)
	var m *Msg
	switch mType {
	case status:
		m = BuildStatusMsg(signer, set)
	case proposal:
		m = BuildProposalMsg(signer, set)
	case commit:
		m = BuildCommitMsg(signer, set)
	case notify:
		m = BuildNotifyMsg(signer, set)
	default:
		panic("unexpected msg type")
	}

	for i, round := range rounds {
		m.Round = msgK
		e := v.ContextuallyValidateMessage(context.Background(), m, round)
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
