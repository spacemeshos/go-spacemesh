package hare

import (
	"context"
	"errors"
	"testing"

	"github.com/golang/mock/gomock"
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
	pke, err := signing.NewPubKeyExtractor()
	require.NoError(tb, err)

	return newSyntaxContextValidator(signer, pke, lowThresh10, trueValidator,
		sq, truer{}, newPubGetter(), NewEligibilityTracker(lowThresh10), logtest.New(tb),
	)
}

func TestMessageValidator_CommitStatus(t *testing.T) {
	signer1, err := signing.NewEdSigner()
	require.NoError(t, err)
	signer2, err := signing.NewEdSigner()
	require.NoError(t, err)

	require.True(t, validateCommitType(BuildCommitMsg(signer1, NewEmptySet(lowDefaultSize))))
	require.True(t, validateStatusType(BuildStatusMsg(signer2, NewEmptySet(lowDefaultSize))))
}

func TestMessageValidator_ValidateCertificate(t *testing.T) {
	sv := defaultValidator(t)
	require.False(t, sv.validateCertificate(context.Background(), nil))
	cert := &Certificate{}
	require.False(t, sv.validateCertificate(context.Background(), cert))
	cert.AggMsgs = &AggregatedMessages{}
	require.False(t, sv.validateCertificate(context.Background(), cert))
	msgs := make([]Message, 0, sv.threshold)
	cert.AggMsgs.Messages = msgs
	require.False(t, sv.validateCertificate(context.Background(), cert))
	msgs = append(msgs, Message{})
	cert.AggMsgs.Messages = msgs
	require.False(t, sv.validateCertificate(context.Background(), cert))
	valueSet := NewSetFromValues(types.ProposalID{1})
	cert.Values = valueSet.ToSlice()
	require.False(t, sv.validateCertificate(context.Background(), cert))

	msgs = make([]Message, 0, sv.threshold)
	for i := 0; i < sv.threshold; i++ {
		signer, err := signing.NewEdSigner()
		require.NoError(t, err)
		// wrong set value
		m := BuildCommitMsg(signer, NewDefaultEmptySet()).Message
		msgs = append(msgs, m)
	}
	cert.AggMsgs.Messages = msgs
	require.False(t, sv.validateCertificate(context.Background(), cert))

	msgs = make([]Message, 0, sv.threshold)
	identities := make(map[types.NodeID]struct{})
	for i := 0; i < sv.threshold; i++ {
		signer, err := signing.NewEdSigner()
		require.NoError(t, err)
		m := BuildCommitMsg(signer, valueSet).Message
		msgs = append(msgs, m)
		identities[signer.NodeID()] = struct{}{}
	}
	cert.AggMsgs.Messages = msgs
	require.True(t, sv.validateCertificate(context.Background(), cert))

	sv.eTracker.ForEach(commitRound, func(s types.NodeID, cred *Cred) {
		_, ok := identities[s]
		require.True(t, ok)
		require.True(t, cred.Honest)
		require.EqualValues(t, 1, cred.Count)
	})
}

func TestEligibilityValidator_validateRole_FailedToValidate(t *testing.T) {
	ctrl := gomock.NewController(t)
	mo := mocks.NewMockRolacle(ctrl)
	ev := newEligibilityValidator(mo, 1, 5, logtest.New(t))

	signer, err := signing.NewEdSigner()
	require.NoError(t, err)

	m := BuildPreRoundMsg(signer, NewDefaultEmptySet(), types.RandomVrfSignature())
	m.Layer = types.NewLayerID(111)
	myErr := errors.New("my error")

	mo.EXPECT().Validate(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return(false, myErr).Times(1)
	res := ev.Validate(context.Background(), m)
	require.False(t, res)
}

func TestEligibilityValidator_validateRole_NotEligible(t *testing.T) {
	ctrl := gomock.NewController(t)
	mo := mocks.NewMockRolacle(ctrl)
	ev := newEligibilityValidator(mo, 1, 5, logtest.New(t))

	signer, err := signing.NewEdSigner()
	require.NoError(t, err)

	m := BuildPreRoundMsg(signer, NewDefaultEmptySet(), types.RandomVrfSignature())
	m.Layer = types.NewLayerID(111)

	mo.EXPECT().Validate(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return(false, nil).Times(1)
	res := ev.Validate(context.Background(), m)
	require.False(t, res)
}

func TestEligibilityValidator_validateRole_Success(t *testing.T) {
	ctrl := gomock.NewController(t)
	mo := mocks.NewMockRolacle(ctrl)
	ev := newEligibilityValidator(mo, 1, 5, logtest.New(t))

	signer, err := signing.NewEdSigner()
	require.NoError(t, err)

	m := BuildPreRoundMsg(signer, NewDefaultEmptySet(), types.RandomVrfSignature())
	m.Layer = types.NewLayerID(111)

	mo.EXPECT().Validate(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return(true, nil).Times(1)
	res := ev.Validate(context.Background(), m)
	require.True(t, res)
}

func TestMessageValidator_IsStructureValid(t *testing.T) {
	sv := defaultValidator(t)

	signer, err := signing.NewEdSigner()
	require.NoError(t, err)

	require.False(t, sv.SyntacticallyValidateMessage(context.Background(), nil))
	m := &Msg{Message: Message{}, NodeID: types.EmptyNodeID}
	require.False(t, sv.SyntacticallyValidateMessage(context.Background(), m))
	m.NodeID = signer.NodeID()
	require.False(t, sv.SyntacticallyValidateMessage(context.Background(), m))

	// empty set is allowed now
	m.InnerMsg = &InnerMessage{}
	require.True(t, sv.SyntacticallyValidateMessage(context.Background(), m))
	m.InnerMsg.Values = nil
	require.True(t, sv.SyntacticallyValidateMessage(context.Background(), m))
	m.InnerMsg.Values = []types.ProposalID{}
	require.True(t, sv.SyntacticallyValidateMessage(context.Background(), m))
}

type mockValidator struct {
	res bool
}

func (m mockValidator) Validate(context.Context, *Msg) bool {
	return m.res
}

func initPg(tb testing.TB, validator *syntaxContextValidator) (*pubGetter, []Message, *signing.EdSigner) {
	pg := newPubGetter()
	msgs := make([]Message, validator.threshold)
	var signer *signing.EdSigner
	for i := 0; i < validator.threshold; i++ {
		var err error
		signer, err = signing.NewEdSigner()
		require.NoError(tb, err)
		iMsg := BuildStatusMsg(signer, NewSetFromValues(types.ProposalID{1}))
		validator.eTracker.Track(iMsg.NodeID, iMsg.Round, iMsg.Eligibility.Count, true)
		msgs[i] = iMsg.Message
		pg.Track(iMsg)
	}
	return pg, msgs, signer
}

func TestMessageValidator_Aggregated(t *testing.T) {
	r := require.New(t)
	sv := defaultValidator(t)
	r.Equal(errNilValidators, sv.validateAggregatedMessage(context.Background(), nil, nil))
	funcs := make([]func(m *Msg) bool, 0)
	r.Equal(errNilAggMsgs, sv.validateAggregatedMessage(context.Background(), nil, funcs))

	agg := &AggregatedMessages{}
	r.Equal(errNilMsgsSlice, sv.validateAggregatedMessage(context.Background(), agg, funcs))

	pg, msgs, sgn := initPg(t, sv)

	sv.validMsgsTracker = pg
	agg.Messages = msgs[:sv.threshold-1]
	r.ErrorIs(sv.validateAggregatedMessage(context.Background(), agg, funcs), errMsgsCountMismatch)

	agg.Messages = msgs
	r.NoError(sv.validateAggregatedMessage(context.Background(), agg, funcs))

	sv.validMsgsTracker = newPubGetter()
	tmp := msgs[0].Signature
	msgs[0].Signature = types.RandomEdSignature()
	r.Error(sv.validateAggregatedMessage(context.Background(), agg, funcs))

	msgs[0].Signature = tmp
	inner := msgs[0].InnerMsg.Values
	msgs[0].InnerMsg.Values = inner
	sv.roleValidator = &mockValidator{}
	r.Equal(errInnerEligibility, sv.validateAggregatedMessage(context.Background(), agg, funcs))

	sv.roleValidator = &mockValidator{true}
	funcs = make([]func(m *Msg) bool, 1)
	funcs[0] = func(m *Msg) bool { return false }
	r.Equal(errInnerFunc, sv.validateAggregatedMessage(context.Background(), agg, funcs))

	funcs[0] = func(m *Msg) bool { return true }
	m0 := msgs[0]
	msgs[0] = BuildStatusMsg(sgn, NewSetFromValues(types.ProposalID{1})).Message
	r.Equal(errDupSender, sv.validateAggregatedMessage(context.Background(), agg, funcs))

	sv.validMsgsTracker = pg
	msgs[0] = m0
	msgs[len(msgs)-1] = m0
	r.Equal(errDupSender, sv.validateAggregatedMessage(context.Background(), agg, funcs))

	signer, err := signing.NewEdSigner()
	require.NoError(t, err)

	msgs[0] = BuildStatusMsg(signer, NewSetFromValues(types.ProposalID{1})).Message
	r.Equal(types.EmptyNodeID, pg.NodeID(&msgs[0]))
	require.NoError(t, sv.validateAggregatedMessage(context.Background(), agg, funcs))
	r.NotEqual(types.EmptyNodeID, pg.NodeID(&msgs[0]))
}

func TestMessageValidator_Aggregated_WithEquivocation(t *testing.T) {
	r := require.New(t)
	sv := defaultValidator(t)
	funcs := make([]func(m *Msg) bool, 0)

	pg, msgs, _ := initPg(t, sv)

	sv.validMsgsTracker = pg

	agg := &AggregatedMessages{Messages: msgs[:sv.threshold-1]}
	r.ErrorIs(sv.validateAggregatedMessage(context.Background(), agg, funcs), errMsgsCountMismatch)

	// add an eligible known equivocator
	ke, err := signing.NewEdSigner()
	require.NoError(t, err)

	sv.eTracker.Track(ke.NodeID(), msgs[0].Round, 1, false)
	r.NoError(sv.validateAggregatedMessage(context.Background(), agg, funcs))
}

func TestSyntaxContextValidator_PreRoundContext(t *testing.T) {
	r := require.New(t)
	validator := defaultValidator(t)
	signer, err := signing.NewEdSigner()
	require.NoError(t, err)
	pre := BuildPreRoundMsg(signer, NewDefaultEmptySet(), types.RandomVrfSignature())
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
	pre := BuildPreRoundMsg(signer, set, types.RandomVrfSignature())
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
	require.Nil(t, err)
	preround := b.SetType(pre).Sign(proc.signer).Build()
	preround.NodeID = proc.signer.NodeID()
	require.True(t, v.SyntacticallyValidateMessage(context.Background(), preround))
	e := v.ContextuallyValidateMessage(context.Background(), preround, 0)
	require.Nil(t, e)
	b, err = proc.initDefaultBuilder(proc.value)
	require.Nil(t, err)
	status := b.SetType(status).Sign(proc.signer).Build()
	status.NodeID = proc.signer.NodeID()
	e = v.ContextuallyValidateMessage(context.Background(), status, 0)
	require.Nil(t, e)
	require.True(t, v.SyntacticallyValidateMessage(context.Background(), status))
}

type pubGetter struct {
	mp map[types.EdSignature]types.NodeID
}

func newPubGetter() *pubGetter {
	return &pubGetter{make(map[types.EdSignature]types.NodeID)}
}

func (pg pubGetter) Track(m *Msg) {
	pg.mp[m.Signature] = m.NodeID
}

func (pg pubGetter) NodeID(m *Message) types.NodeID {
	if pg.mp == nil {
		return types.EmptyNodeID
	}

	p, ok := pg.mp[m.Signature]
	if !ok {
		return types.EmptyNodeID
	}

	return p
}

func TestMessageValidator_SyntacticallyValidateMessage(t *testing.T) {
	signer, err := signing.NewEdSigner()
	require.NoError(t, err)
	pke, err := signing.NewPubKeyExtractor()
	require.NoError(t, err)
	et := NewEligibilityTracker(100)
	vfunc := func(m *Msg) bool { return true }

	sv := newSyntaxContextValidator(signer, pke, 1, vfunc, nil, truer{}, newPubGetter(), et, logtest.New(t))
	m := BuildPreRoundMsg(signer, NewDefaultEmptySet(), types.RandomVrfSignature())
	require.True(t, sv.SyntacticallyValidateMessage(context.Background(), m))
	m = BuildPreRoundMsg(signer, NewSetFromValues(types.ProposalID{1}), types.RandomVrfSignature())
	require.True(t, sv.SyntacticallyValidateMessage(context.Background(), m))
}

func TestMessageValidator_validateSVPTypeA(t *testing.T) {
	signer, err := signing.NewEdSigner()
	require.NoError(t, err)

	m := buildProposalMsg(signer, NewSetFromValues(types.ProposalID{1}, types.ProposalID{2}, types.ProposalID{3}), types.RandomVrfSignature())
	s1 := NewSetFromValues(types.ProposalID{1})
	s2 := NewSetFromValues(types.ProposalID{3})
	s3 := NewSetFromValues(types.ProposalID{1}, types.ProposalID{5})
	s4 := NewSetFromValues(types.ProposalID{1}, types.ProposalID{4})
	v := defaultValidator(t)
	m.InnerMsg.Svp = buildSVP(preRound, s1, s2, s3, s4)
	require.False(t, v.validateSVPTypeA(context.Background(), m))
	s3 = NewSetFromValues(types.ProposalID{2})
	m.InnerMsg.Svp = buildSVP(preRound, s1, s2, s3)
	require.True(t, v.validateSVPTypeA(context.Background(), m))
}

func TestMessageValidator_validateSVPTypeB(t *testing.T) {
	signer, err := signing.NewEdSigner()
	require.NoError(t, err)

	m := buildProposalMsg(signer, NewSetFromValues(types.ProposalID{1}, types.ProposalID{2}, types.ProposalID{3}), types.RandomVrfSignature())
	s1 := NewSetFromValues(types.ProposalID{1})
	m.InnerMsg.Svp = buildSVP(preRound, s1)
	m.InnerMsg.Values = NewSetFromValues(types.ProposalID{1}).ToSlice()
	v := defaultValidator(t)
	require.False(t, v.validateSVPTypeB(context.Background(), m, NewSetFromValues(types.ProposalID{5})))
	require.True(t, v.validateSVPTypeB(context.Background(), m, NewSetFromValues(types.ProposalID{1})))
}

func TestMessageValidator_validateSVP(t *testing.T) {
	signer, err := signing.NewEdSigner()
	require.NoError(t, err)
	pke, err := signing.NewPubKeyExtractor()
	require.NoError(t, err)

	ctrl := gomock.NewController(t)
	mockStateQ := mocks.NewMockstateQuerier(ctrl)
	mockStateQ.EXPECT().IsIdentityActiveOnConsensusView(gomock.Any(), gomock.Any(), gomock.Any()).Return(true, nil).AnyTimes()
	et := NewEligibilityTracker(100)
	vfunc := func(m *Msg) bool { return true }
	sv := newSyntaxContextValidator(signer, pke, 1, vfunc, mockStateQ, truer{}, newPubGetter(), et, logtest.New(t))
	m := buildProposalMsg(signer, NewSetFromValues(types.ProposalID{1}, types.ProposalID{2}, types.ProposalID{3}), types.RandomVrfSignature())
	s1 := NewSetFromValues(types.ProposalID{1})
	m.InnerMsg.Svp = buildSVP(preRound, s1)
	m.InnerMsg.Svp.Messages[0].InnerMsg.Type = commit
	require.False(t, sv.validateSVP(context.Background(), m))
	m.InnerMsg.Svp = buildSVP(preRound, s1)
	m.InnerMsg.Svp.Messages[0].Round = 4
	require.False(t, sv.validateSVP(context.Background(), m))
	m.InnerMsg.Svp = buildSVP(preRound, s1)
	require.False(t, sv.validateSVP(context.Background(), m))
	s2 := NewSetFromValues(types.ProposalID{1}, types.ProposalID{2}, types.ProposalID{3})
	m.InnerMsg.Svp = buildSVP(preRound, s2)
	require.True(t, sv.validateSVP(context.Background(), m))
	m.InnerMsg.Svp = buildSVP(0, s1)
	require.False(t, sv.validateSVP(context.Background(), m))
	m.InnerMsg.Svp = buildSVP(0, s2)
	require.True(t, sv.validateSVP(context.Background(), m))
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
