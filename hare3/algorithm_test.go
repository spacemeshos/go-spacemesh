package hare3

import (
	"crypto/rand"
	"testing"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/stretchr/testify/require"
)

var (
	id1 = randID()
	id2 = randID()
	id3 = randID()

	val1 = randHash20()
	val2 = randHash20()
	val3 = randHash20()

	msgHash1 = randHash20()

	values1 = sortHash20([]types.Hash20{val1})
	values2 = sortHash20([]types.Hash20{val1, val2})
	values3 = sortHash20([]types.Hash20{val1, val2, val3})

	rPre = NewAbsRound(0, -1)
	r0   = NewAbsRound(0, 0)
	r1   = NewAbsRound(0, 1)
	r2   = NewAbsRound(0, 2)
	r3   = NewAbsRound(0, 3)
	r4   = NewAbsRound(0, 4)
)

func randID() types.NodeID {
	var result types.NodeID
	rand.Read(result[:])
	return result
}

func randHash20() types.Hash20 {
	var result types.Hash20
	rand.Read(result[:])
	return result
}

type TestRoundProvider struct {
	round AbsRound
}

func NewTestRoundProvider(round AbsRound) *TestRoundProvider { return &TestRoundProvider{round: round} }

// CurrentRound implements RoundProvider.
func (rp *TestRoundProvider) CurrentRound() AbsRound {
	return rp.round
}

type TestLeaderChecker struct{}

func NewTestLeaderChecker() *TestLeaderChecker { return &TestLeaderChecker{} }

// IsLeader implements LeaderChecker.
func (lc *TestLeaderChecker) IsLeader(vk types.NodeID, round AbsRound) bool {
	return true
}

// This test checks that a single node with a threshold of 1 vote can reach
// consensus on a value, in order for the protocol to complete it is required
// to run 2 iterations (15 rounds in total).
func TestReachingConsensusSingleNode(t *testing.T) {
	h := NewHandler(NewDefaultGradedGossiper(), NewDefaultThresholdGradedGossiper(1), NewDefaultGradecaster())
	lc := NewTestLeaderChecker()
	nodeId := randID()
	active := true
	values3Hash := []types.Hash20{toHash(values3)}
	p := h.Protocol(lc, values3)

	msg, output := p.NextRound(active)
	require.Equal(t, &OutputMessage{NewAbsRound(0, -1), values3}, msg)
	require.Nil(t, output)

	regossip, equivocationHash := h.HandleMsg(randHash20(), nodeId, msg.Round, msg.Values)
	require.Equal(t, true, regossip)
	require.Nil(t, equivocationHash)

	msg, output = p.NextRound(active)
	require.Nil(t, msg)
	require.Nil(t, output)

	msg, output = p.NextRound(active)
	require.Nil(t, msg)
	require.Nil(t, output)

	msg, output = p.NextRound(active)
	require.Equal(t, &OutputMessage{NewAbsRound(0, 2), values3}, msg)
	require.Nil(t, output)

	regossip, equivocationHash = h.HandleMsg(randHash20(), nodeId, msg.Round, msg.Values)
	require.Equal(t, true, regossip)
	require.Nil(t, equivocationHash)

	msg, output = p.NextRound(active)
	require.Nil(t, msg)
	require.Nil(t, output)

	msg, output = p.NextRound(active)
	require.Nil(t, msg)
	require.Nil(t, output)

	msg, output = p.NextRound(active)
	require.Equal(t, &OutputMessage{NewAbsRound(0, 5), values3Hash}, msg)
	require.Nil(t, output)

	regossip, equivocationHash = h.HandleMsg(randHash20(), nodeId, msg.Round, msg.Values)
	require.Equal(t, true, regossip)
	require.Nil(t, equivocationHash)

	msg, output = p.NextRound(active)
	require.Equal(t, &OutputMessage{NewAbsRound(0, 6), values3Hash}, msg)
	require.Nil(t, output)

	regossip, equivocationHash = h.HandleMsg(randHash20(), nodeId, msg.Round, msg.Values)
	require.Equal(t, true, regossip)
	require.Nil(t, equivocationHash)

	msg, output = p.NextRound(active)
	require.Nil(t, msg)
	require.Nil(t, output)

	msg, output = p.NextRound(active)
	require.Nil(t, msg)
	require.Nil(t, output)

	msg, output = p.NextRound(active)
	require.Equal(t, &OutputMessage{NewAbsRound(1, 2), values3}, msg)
	require.Nil(t, output)

	msg, output = p.NextRound(active)
	require.Nil(t, msg)
	require.Nil(t, output)

	msg, output = p.NextRound(active)
	require.Nil(t, msg)
	require.Nil(t, output)

	msg, output = p.NextRound(active)
	require.Equal(t, &OutputMessage{NewAbsRound(1, 5), values3Hash}, msg)
	require.Nil(t, output)

	msg, output = p.NextRound(active)
	require.Equal(t, &OutputMessage{NewAbsRound(1, 6), values3Hash}, msg)
	require.Equal(t, values3, output)

	regossip, equivocationHash = h.HandleMsg(randHash20(), nodeId, msg.Round, msg.Values)
	require.Equal(t, true, regossip)
	require.Nil(t, equivocationHash)
}

type TestNetwork struct {
	protocols []*Protocol
	handlers  []*Handler
	ids       []types.NodeID
}

func NewTestNetwork(threshold uint16, initialValues ...[]types.Hash20) *TestNetwork {
	lc := NewTestLeaderChecker()
	tn := &TestNetwork{}
	for i := 0; i < len(initialValues); i++ {
		tn.handlers = append(tn.handlers, NewHandler(NewDefaultGradedGossiper(), NewDefaultThresholdGradedGossiper(threshold), NewDefaultGradecaster()))
		tn.protocols = append(tn.protocols, tn.handlers[i].Protocol(lc, initialValues[i]))
		tn.ids = append(tn.ids, randID())
	}
	return tn
}

func (tn *TestNetwork) NextRound() (msgs []*OutputMessage, outputs [][]types.Hash20) {
	for _, p := range tn.protocols {
		msg, output := p.NextRound(true)
		msgs = append(msgs, msg)
		outputs = append(outputs, output)
	}
	return msgs, outputs
}

func (tn *TestNetwork) HandleMsgs(msgs []*OutputMessage) (regossip []bool, equivocation []*types.Hash20) {
	for i, m := range msgs {
		msgHash := randHash20()
		id := tn.ids[i]
		for _, h := range tn.handlers {
			rg, eq := h.HandleMsg(msgHash, id, m.Round, m.Values)
			regossip = append(regossip, rg)
			equivocation = append(equivocation, eq)
		}
	}
	return regossip, equivocation
}

func TestReachingConsensusNetworkOf2(t *testing.T) {
	n := NewTestNetwork(2, values3, values3)
	values3Hash := []types.Hash20{toHash(values3)}

	msgs, outputs := n.NextRound()
	expected := &OutputMessage{NewAbsRound(0, -1), values3}
	require.Equal(t, []*OutputMessage{expected, expected}, msgs)
	require.Equal(t, [][]types.Hash20{nil, nil}, outputs)

	regossip, equivocation := n.HandleMsgs(msgs)
	require.Equal(t, []bool{true, true, true, true}, regossip)
	require.Equal(t, []*types.Hash20{nil, nil, nil, nil}, equivocation)

	msgs, outputs = n.NextRound()
	expected = nil
	require.Equal(t, []*OutputMessage{expected, expected}, msgs)
	require.Equal(t, [][]types.Hash20{nil, nil}, outputs)

	msgs, outputs = n.NextRound()
	expected = nil
	require.Equal(t, []*OutputMessage{expected, expected}, msgs)
	require.Equal(t, [][]types.Hash20{nil, nil}, outputs)

	msgs, outputs = n.NextRound() // Round 2
	expected = &OutputMessage{NewAbsRound(0, 2), values3}
	require.Equal(t, []*OutputMessage{expected, expected}, msgs)
	require.Equal(t, [][]types.Hash20{nil, nil}, outputs)

	regossip, equivocation = n.HandleMsgs(msgs)
	require.Equal(t, []bool{true, true, true, true}, regossip)
	require.Equal(t, []*types.Hash20{nil, nil, nil, nil}, equivocation)

	msgs, outputs = n.NextRound()
	expected = nil
	require.Equal(t, []*OutputMessage{expected, expected}, msgs)
	require.Equal(t, [][]types.Hash20{nil, nil}, outputs)

	msgs, outputs = n.NextRound()
	expected = nil
	require.Equal(t, []*OutputMessage{expected, expected}, msgs)
	require.Equal(t, [][]types.Hash20{nil, nil}, outputs)

	msgs, outputs = n.NextRound() // Round 5
	expected = &OutputMessage{NewAbsRound(0, 5), values3Hash}
	require.Equal(t, []*OutputMessage{expected, expected}, msgs)
	require.Equal(t, [][]types.Hash20{nil, nil}, outputs)

	regossip, equivocation = n.HandleMsgs(msgs)
	require.Equal(t, []bool{true, true, true, true}, regossip)
	require.Equal(t, []*types.Hash20{nil, nil, nil, nil}, equivocation)

	msgs, outputs = n.NextRound() // Round 6
	expected = &OutputMessage{NewAbsRound(0, 6), values3Hash}
	require.Equal(t, []*OutputMessage{expected, expected}, msgs)
	require.Equal(t, [][]types.Hash20{nil, nil}, outputs)

	regossip, equivocation = n.HandleMsgs(msgs)
	require.Equal(t, []bool{true, true, true, true}, regossip)
	require.Equal(t, []*types.Hash20{nil, nil, nil, nil}, equivocation)

	msgs, outputs = n.NextRound() // Round 0 iteration 1
	expected = nil
	require.Equal(t, []*OutputMessage{expected, expected}, msgs)
	require.Equal(t, [][]types.Hash20{nil, nil}, outputs)

	msgs, outputs = n.NextRound()
	expected = nil
	require.Equal(t, []*OutputMessage{expected, expected}, msgs)
	require.Equal(t, [][]types.Hash20{nil, nil}, outputs)

	msgs, outputs = n.NextRound() // Round 2 iteration 1
	expected = &OutputMessage{NewAbsRound(1, 2), values3}
	require.Equal(t, []*OutputMessage{expected, expected}, msgs)
	require.Equal(t, [][]types.Hash20{nil, nil}, outputs)

	regossip, equivocation = n.HandleMsgs(msgs)
	require.Equal(t, []bool{true, true, true, true}, regossip)
	require.Equal(t, []*types.Hash20{nil, nil, nil, nil}, equivocation)

	msgs, outputs = n.NextRound()
	expected = nil
	require.Equal(t, []*OutputMessage{expected, expected}, msgs)
	require.Equal(t, [][]types.Hash20{nil, nil}, outputs)

	msgs, outputs = n.NextRound()
	expected = nil
	require.Equal(t, []*OutputMessage{expected, expected}, msgs)
	require.Equal(t, [][]types.Hash20{nil, nil}, outputs)

	msgs, outputs = n.NextRound() // Round 5 iteration 1
	expected = &OutputMessage{NewAbsRound(1, 5), values3Hash}
	require.Equal(t, []*OutputMessage{expected, expected}, msgs)
	require.Equal(t, [][]types.Hash20{nil, nil}, outputs)

	regossip, equivocation = n.HandleMsgs(msgs)
	require.Equal(t, []bool{true, true, true, true}, regossip)
	require.Equal(t, []*types.Hash20{nil, nil, nil, nil}, equivocation)

	msgs, outputs = n.NextRound() // Round 6 iteration 1
	expected = &OutputMessage{NewAbsRound(1, 6), values3Hash}
	require.Equal(t, []*OutputMessage{expected, expected}, msgs)
	require.Equal(t, [][]types.Hash20{values3, values3}, outputs)

	regossip, equivocation = n.HandleMsgs(msgs)
	require.Equal(t, []bool{true, true, true, true}, regossip)
	require.Equal(t, []*types.Hash20{nil, nil, nil, nil}, equivocation)
}

// This test checks that a single node with a threshold of 2 votes can't reach
// consensus on a value
func TestNotReachingConsensus(t *testing.T) {
	h := NewHandler(NewDefaultGradedGossiper(), NewDefaultThresholdGradedGossiper(2), NewDefaultGradecaster())
	lc := NewTestLeaderChecker()
	nodeId := randID()
	active := true
	// values3Hash := []types.Hash20{toHash(values3)}
	p := h.Protocol(lc, values3)

	// We expect just the pre round message to be sent
	msg, output := p.NextRound(active)
	require.Equal(t, &OutputMessage{NewAbsRound(0, -1), values3}, msg)
	require.Nil(t, output)

	regossip, equivocationHash := h.HandleMsg(randHash20(), nodeId, msg.Round, msg.Values)
	require.Equal(t, true, regossip)
	require.Nil(t, equivocationHash)

	maxRound := NewAbsRound(3, 0)

	for p.Round() < maxRound {
		msg, output = p.NextRound(active)
		require.Nil(t, msg)
		require.Nil(t, output)
	}
}
