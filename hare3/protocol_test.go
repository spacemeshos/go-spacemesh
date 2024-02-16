package hare3

import (
	"testing"

	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/log/logtest"
)

func castIds(strings ...string) []types.ProposalID {
	ids := []types.ProposalID{}
	for _, p := range strings {
		var id types.ProposalID
		copy(id[:], p)
		ids = append(ids, id)
	}
	return ids
}

type tinput struct {
	input
	expect *response
}

type response struct {
	gossip       bool
	equivocation *types.HareProof
}

func (t *tinput) ensureMsg() {
	if t.Message == nil {
		t.Message = &Message{}
	}
}

func (t *tinput) ensureResponse() {
	if t.expect == nil {
		t.expect = &response{}
	}
}

func (t *tinput) round(r Round) *tinput {
	t.ensureMsg()
	t.Round = r
	return t
}

func (t *tinput) iter(i uint8) *tinput {
	t.ensureMsg()
	t.Iter = i
	return t
}

func (t *tinput) proposals(proposals ...string) *tinput {
	t.ensureMsg()
	t.Value.Proposals = castIds(proposals...)
	return t
}

func (t *tinput) ref(proposals ...string) *tinput {
	t.ensureMsg()
	hs := types.CalcProposalHash32Presorted(castIds(proposals...), nil)
	t.Value.Reference = &hs
	return t
}

func (t *tinput) vrf(vrf ...byte) *tinput {
	t.ensureMsg()
	copy(t.Eligibility.Proof[:], vrf)
	return t
}

func (t *tinput) vrfcount(c uint16) *tinput {
	t.ensureMsg()
	t.Eligibility.Count = c
	return t
}

func (t *tinput) sender(name string) *tinput {
	t.ensureMsg()
	copy(t.Sender[:], name)
	return t
}

func (t *tinput) mshHash(h string) *tinput {
	copy(t.input.msgHash[:], h)
	return t
}

func (t *tinput) malicious() *tinput {
	t.input.malicious = true
	return t
}

func (t *tinput) gossip() *tinput {
	t.ensureResponse()
	t.expect.gossip = true
	return t
}

func (t *tinput) equi() *tinput {
	// TODO(dshulyak) do i want to test that it constructed correctly here?
	t.ensureResponse()
	t.expect.equivocation = &types.HareProof{}
	return t
}

func (t *tinput) nogossip() *tinput {
	t.ensureResponse()
	t.expect.gossip = false
	return t
}

func (t *tinput) g(g grade) *tinput {
	t.atxgrade = g
	return t
}

type toutput struct {
	act bool
	output
}

func (t *toutput) ensureMsg() {
	if t.message == nil {
		t.message = &Message{}
	}
}

func (t *toutput) active() *toutput {
	t.act = true
	return t
}

func (t *toutput) round(r Round) *toutput {
	t.ensureMsg()
	t.message.Round = r
	return t
}

func (t *toutput) iter(i uint8) *toutput {
	t.ensureMsg()
	t.message.Iter = i
	return t
}

func (t *toutput) proposals(proposals ...string) *toutput {
	t.ensureMsg()
	t.message.Value.Proposals = castIds(proposals...)
	return t
}

func (t *toutput) ref(proposals ...string) *toutput {
	t.ensureMsg()
	hs := types.CalcProposalHash32Presorted(castIds(proposals...), nil)
	t.message.Value.Reference = &hs
	return t
}

func (t *toutput) terminated() *toutput {
	t.output.terminated = true
	return t
}

func (t *toutput) coin(c bool) *toutput {
	t.output.coin = &c
	return t
}

func (t *toutput) result(proposals ...string) *toutput {
	t.output.result = castIds(proposals...)
	return t
}

type setup struct {
	threshold uint16
	proposals []types.ProposalID
}

func (s *setup) thresh(v uint16) *setup {
	s.threshold = v
	return s
}

func (s *setup) initial(proposals ...string) *setup {
	s.proposals = castIds(proposals...)
	return s
}

type testCase struct {
	desc  string
	steps []any
}

func gen(desc string, steps ...any) testCase {
	return testCase{desc: desc, steps: steps}
}

func TestProtocol(t *testing.T) {
	for _, tc := range []testCase{
		gen("sanity", // simplest e2e protocol run
			new(setup).thresh(10).initial("a", "b"),
			new(toutput).active().round(preround).proposals("a", "b"),
			new(tinput).sender("1").round(preround).proposals("b", "a").vrfcount(3).g(grade5),
			new(tinput).sender("2").round(preround).proposals("a", "c").vrfcount(9).g(grade5),
			new(tinput).sender("3").round(preround).proposals("c").vrfcount(6).g(grade5),
			new(toutput).coin(false),
			new(toutput).active().round(propose).proposals("a", "c"),
			new(tinput).sender("1").round(propose).proposals("a", "c").g(grade5).vrf(2),
			new(tinput).sender("2").round(propose).proposals("b", "d").g(grade5).vrf(1),
			new(toutput),
			new(toutput),
			new(toutput).active().round(commit).ref("a", "c"),
			new(tinput).sender("1").round(commit).ref("a", "c").vrfcount(4).g(grade5),
			new(tinput).sender("2").round(commit).ref("a", "c").vrfcount(8).g(grade5),
			new(toutput).active().round(notify).ref("a", "c"),
			new(tinput).sender("1").round(notify).ref("a", "c").vrfcount(5).g(grade5),
			new(tinput).sender("2").round(notify).ref("a", "c").vrfcount(6).g(grade5),
			new(toutput).result("a", "c"), // hardlock
			new(toutput),                  // softlock
			// propose, commit, notify messages are built based on prervious state
			new(toutput).active().round(propose).iter(1).proposals("a", "c"), // propose
			new(toutput), // wait1
			new(toutput), // wait2
			new(toutput).active().round(commit).iter(1).ref("a", "c"), // commit
			new(toutput).active().round(notify).iter(1).ref("a", "c"), // notify
			new(toutput).terminated(),
		),
		gen("commit on softlock",
			new(setup).thresh(10).initial("a", "b"),
			new(toutput),
			new(tinput).sender("1").round(preround).proposals("a", "b").vrfcount(11).g(grade5),
			new(toutput).coin(false),
			new(toutput),
			new(tinput).sender("1").round(propose).proposals("a", "b").g(grade5),
			new(tinput).sender("2").round(propose).proposals("b").g(grade5),
			new(toutput),
			new(toutput),
			new(toutput),
			new(tinput).sender("1").round(commit).ref("b").vrfcount(11).g(grade3),
			new(toutput),
			new(toutput), // hardlock
			new(toutput), // softlock
			// propose, commit, notify messages are built based on prervious state
			new(toutput), // propose
			new(tinput).sender("1").iter(1).round(propose).proposals("b").g(grade5),
			new(toutput), // wait1
			new(toutput), // wait2
			new(toutput).active().round(commit).iter(1).ref("b"), // commit
		),
		gen("empty 0 iteration", // test that protocol can complete not only in 0st iteration
			new(setup).thresh(10).initial("a", "b"),
			new(toutput), // preround
			new(tinput).sender("1").round(preround).proposals("a", "b").vrfcount(3).g(grade5),
			new(tinput).sender("2").round(preround).proposals("a", "b").vrfcount(9).g(grade5),
			new(toutput).coin(false), // softlock
			new(toutput),             // propose
			new(toutput),             // wait1
			new(toutput),             // wait2
			new(toutput),             // commit
			new(toutput),             // notify
			new(toutput),             // 2nd hardlock
			new(toutput),             // 2nd softlock
			new(toutput).active().iter(1).round(propose).proposals("a", "b"), // 2nd propose
			new(tinput).sender("1").iter(1).round(propose).proposals("a", "b").g(grade5),
			new(toutput), // 2nd wait1
			new(toutput), // 2nd wait2
			new(toutput).active().iter(1).round(commit).ref("a", "b"),
			new(tinput).sender("1").iter(1).round(commit).ref("a", "b").g(grade5).vrfcount(11),
			new(toutput).active().iter(1).round(notify).ref("a", "b"),
			new(tinput).sender("1").iter(1).round(notify).ref("a", "b").g(grade5).vrfcount(11),
			new(toutput).result("a", "b"), // 3rd hardlock
			new(toutput),                  // 3rd softlock
			new(toutput),                  // 3rd propose
			new(toutput),                  // 3rd wait1
			new(toutput),                  // 3rd wait2
			new(toutput),                  // 3rd commit
			new(toutput),                  // 3rd notify
			new(toutput).terminated(),     // 4th softlock
		),
		gen("empty proposal",
			new(setup).thresh(10),
			new(toutput), // preround
			new(tinput).sender("1").round(preround).vrfcount(11).g(grade5),
			new(toutput).coin(false),                         // softlock
			new(toutput).active().round(propose).proposals(), // propose
			new(tinput).sender("1").round(propose).g(grade5).vrf(2),
			new(toutput), // wait1
			new(toutput), // wait2
			new(toutput).active().round(commit).ref(), // commit
			new(tinput).sender("1").round(commit).ref().vrfcount(11).g(grade5),
			new(toutput).active().round(notify).ref(), // notify
			new(tinput).sender("1").round(notify).ref().vrfcount(11).g(grade5),
			new(toutput).result(), // hardlock
		),
		gen("coin true",
			new(setup).thresh(10),
			new(toutput),
			new(tinput).sender("2").round(preround).vrf(2).g(grade5),
			new(tinput).sender("1").round(preround).vrf(1).g(grade5),
			new(tinput).sender("3").round(preround).vrf(2).g(grade5),
			new(toutput).coin(true),
		),
		gen("coin false",
			new(setup).thresh(10),
			new(toutput),
			new(tinput).sender("2").round(preround).vrf(1, 2).g(grade5),
			new(tinput).sender("1").round(preround).vrf(0, 1).g(grade5),
			new(tinput).sender("3").round(preround).vrf(2).g(grade5),
			new(toutput).coin(false),
		),
		gen("coin delayed",
			new(setup).thresh(10),
			new(toutput),
			new(toutput),
			new(tinput).sender("2").round(preround).vrf(1, 2).g(grade5),
			new(tinput).sender("1").round(preround).vrf(2, 3).g(grade5),
			new(toutput).coin(true),
		),
		gen("duplicates don't affect thresholds",
			new(setup).thresh(10),
			new(toutput),
			new(tinput).sender("1").round(preround).proposals("a", "b").vrfcount(5).g(grade5),
			new(tinput).sender("3").round(preround).proposals("d").vrfcount(6).g(grade5).gossip(),
			new(tinput).sender("2").round(preround).proposals("a", "b").vrfcount(6).g(grade5),
			new(tinput).sender("3").round(preround).proposals("d").vrfcount(6).g(grade5),
			new(toutput).coin(false),
			new(toutput).active().round(propose).proposals("a", "b"), // assert that `d` doesn't cross
			new(tinput).sender("1").round(propose).proposals("a").vrf(2).g(grade5),
			// this one would be preferred if duplicates were counted
			new(tinput).sender("2").round(propose).proposals("b").vrf(1).g(grade5),
			new(toutput), // wait1
			new(toutput), // wait2
			new(toutput), // commit
			new(tinput).sender("1").round(commit).ref("a").vrfcount(4).g(grade5),
			new(tinput).sender("2").round(commit).ref("b").vrfcount(6).g(grade5),
			new(tinput).sender("2").round(commit).ref("b").vrfcount(6).g(grade5),
			new(tinput).sender("3").round(commit).ref("a").vrfcount(7).g(grade5),
			new(toutput).active().round(notify).ref("a"), // duplicates commits were ignored
		),
		gen("malicious preround",
			new(setup).thresh(10),
			new(toutput),
			new(tinput).sender("1").round(preround).proposals("a", "b").vrfcount(9).g(grade5),
			new(tinput).sender("2").malicious().gossip().
				round(preround).proposals("b", "d").vrfcount(11).g(grade5),
			new(toutput).coin(false),
			// d would be added if from non-malicious
			new(toutput).active().round(propose).proposals("b"),
		),
		gen("malicious proposal",
			new(setup).thresh(10),
			new(toutput),
			new(toutput), // softlock
			new(tinput).sender("5").
				round(preround).proposals("a", "b", "c").vrfcount(11).g(grade5),
			new(toutput).coin(false), // propose
			new(tinput).sender("1").round(propose).proposals("a", "c").vrf(2).g(grade5),
			// this one would be preferred if from non-malicious
			new(tinput).sender("2").malicious().gossip().
				round(propose).proposals("b").vrf(1).g(grade5),
			new(toutput), // wait1
			new(toutput), // wait2
			new(toutput).active().round(commit).ref("a", "c"), // commit
		),
		gen("malicious commit",
			new(setup).thresh(10),
			new(toutput),
			new(tinput).sender("5").
				round(preround).proposals("a").vrfcount(11).g(grade5),
			new(toutput).coin(false), // softlock
			new(toutput),             // propose
			new(tinput).sender("1").round(propose).proposals("a").g(grade5),
			new(toutput), // wait1
			new(toutput), // wait2
			new(toutput), // commit
			new(tinput).sender("1").malicious().gossip().
				round(commit).ref("a").vrfcount(11).g(grade5),
			new(toutput).active(), // notify outputs nothing
		),
		gen("malicious notify",
			new(setup).thresh(10),
			new(toutput),
			new(tinput).sender("5").
				round(preround).proposals("a").vrfcount(11).g(grade5),
			new(toutput).coin(false), // softlock
			new(toutput),             // propose
			new(tinput).sender("1").round(propose).proposals("a").g(grade5),
			new(toutput), // wait1
			new(toutput), // wait2
			new(toutput), // commit
			new(tinput).sender("1").round(commit).ref("a").vrfcount(11).g(grade5),
			new(toutput), // notify
			new(tinput).sender("1").malicious().gossip().
				round(notify).ref("a").vrfcount(11).g(grade5),
			new(toutput), // no result as the only notify is malicious
		),
		gen("equivocation preround",
			new(setup).thresh(10),
			new(toutput),
			new(tinput).sender("7").mshHash("m0").
				round(preround).proposals("a").vrfcount(11).g(grade4),
			new(tinput).sender("7").gossip().mshHash("m1").equi().
				round(preround).proposals("b").vrfcount(11).g(grade5),
			new(tinput).sender("7").nogossip().mshHash("m2").
				round(preround).proposals("c").vrfcount(11).g(grade3),
		),
		gen("multiple malicious not broadcasted",
			new(setup).thresh(10),
			new(toutput),
			new(tinput).sender("7").malicious().gossip().
				round(preround).proposals("a").vrfcount(11).g(grade5),
			new(tinput).sender("7").malicious().nogossip().
				round(preround).proposals("b").vrfcount(11).g(grade5),
			new(tinput).sender("7").malicious().nogossip().
				round(preround).proposals("c").vrfcount(11).g(grade5),
		),
		gen("no commit for grade1",
			new(setup).thresh(10),
			new(toutput),
			new(tinput).sender("5").
				round(preround).proposals("a").vrfcount(11).g(grade5),
			new(toutput).coin(false), // softlock
			new(toutput),             // propose
			new(toutput),             // wait1
			new(tinput).sender("1").round(propose).proposals("a").g(grade5),
			new(toutput),          // wait2
			new(toutput).active(), // commit
		),
		gen("other gradecast was received",
			new(setup).thresh(10),
			new(toutput),
			new(tinput).sender("5").
				round(preround).proposals("a").vrfcount(11).g(grade5),
			new(toutput).coin(false), // softlock
			new(toutput),             // propose
			new(tinput).sender("1").round(propose).proposals("a").g(grade5),
			new(toutput), // wait1
			new(tinput).sender("1").mshHash("a").gossip().round(propose).proposals("b").g(grade3),
			new(toutput),          // wait2
			new(toutput).active(), // commit
		),
		gen("no commit if not subset of grade3",
			new(setup).thresh(10),
			new(toutput), // preround
			new(toutput), // softlock
			new(toutput), // propose
			new(tinput).sender("1").round(propose).proposals("a").g(grade5),
			new(toutput), // wait1
			new(tinput).sender("1").
				round(preround).proposals("a").vrfcount(11).g(grade2),
			new(toutput).coin(false), // wait2
			new(toutput).active(),    // commit
		),
		gen("grade5 proposals are not in propose",
			new(setup).thresh(10),
			new(toutput), // preround
			new(tinput).sender("1").
				round(preround).proposals("a").vrfcount(11).g(grade5),
			new(toutput).coin(false), // softlock
			new(tinput).sender("2").
				round(preround).proposals("b").vrfcount(11).g(grade5),
			new(toutput), // propose
			new(tinput).sender("1").round(propose).proposals("b").g(grade5),
			new(toutput),          // wait1
			new(toutput),          // wait2
			new(toutput).active(), // commit
		),
		gen("commit locked",
			new(setup).thresh(10),
			new(toutput), // preround
			new(tinput).sender("1").
				round(preround).proposals("a", "b").vrfcount(11).g(grade5),
			new(toutput).coin(false), // softlock
			new(toutput),             // propose
			new(tinput).sender("1").round(propose).proposals("a").g(grade5),
			new(toutput), // wait1
			new(toutput), // wait2
			new(toutput), // commit
			new(toutput), // notify
			new(toutput), // hardlock
			// commit on a will have grade3, so no hardlock
			new(tinput).sender("1").round(commit).ref("a").vrfcount(11).g(grade5),
			new(toutput), // softlock
			new(toutput), // propose
			// commit on b will have grade1, to satisfy condition (g)
			new(tinput).sender("2").round(commit).ref("b").vrfcount(11).g(grade5),
			new(tinput).sender("1").iter(1).round(propose).proposals("a").g(grade5).vrf(2),
			new(tinput).sender("2").iter(1).round(propose).proposals("b").g(grade5).vrf(1),
			new(toutput), // wait1
			new(toutput), // wait2
			// condition (h) ensures that we commit on locked value, even though proposal for b
			// is first in the order
			new(toutput).active().round(commit).iter(1).ref("a"), // commit
		),
		gen("early proposal by one",
			new(setup).thresh(10),
			new(tinput).sender("1").round(preround).proposals("a", "b").vrfcount(11).g(grade5),
			new(toutput).coin(false), // preround
			new(toutput),             // softlock
			new(tinput).sender("1").round(propose).proposals("a", "b").g(grade5).vrf(1),
			new(toutput), // propose
			new(toutput), // wait1
			new(toutput), // wait2
			new(toutput).active().round(commit).ref("a", "b"),
		),
		gen("early proposal by two",
			new(setup).thresh(10),
			new(tinput).sender("1").round(preround).proposals("a", "b").vrfcount(11).g(grade5),
			new(toutput).coin(false), // preround
			new(tinput).sender("1").round(propose).proposals("a", "b").g(grade5).vrf(1),
			new(toutput), // softlock
			new(toutput), // propose
			new(toutput), // wait1
			new(toutput), // wait2
			new(toutput).active().round(commit).ref("a", "b"),
		),
	} {
		t.Run(tc.desc, func(t *testing.T) {
			var (
				proto  *protocol
				logger = logtest.New(t).Zap()
			)
			for i, step := range tc.steps {
				if i != 0 && proto == nil {
					require.FailNow(t, "step with setup should be the first one")
				}
				switch casted := step.(type) {
				case *setup:
					proto = newProtocol(casted.threshold)
					proto.OnInitial(casted.proposals)
				case *tinput:
					logger.Debug("input", zap.Int("i", i), zap.Inline(casted))
					gossip, equivocation := proto.OnInput(&casted.input)
					if casted.expect != nil {
						require.Equal(t, casted.expect.gossip, gossip, "%d", i)
						if casted.expect.equivocation != nil {
							require.NotEmpty(t, equivocation)
						}
					}
				case *toutput:
					before := proto.Round
					out := proto.Next()
					if casted.act {
						require.Equal(t, casted.output, out, "%d", i)
					}
					logger.Debug("output",
						zap.Int("i", i),
						zap.Inline(casted),
						zap.Stringer("before", before),
						zap.Stringer("after", proto.Round),
					)
					stats := proto.Stats()
					enc := zapcore.NewMapObjectEncoder()
					require.NoError(t, stats.MarshalLogObject(enc))
				}
			}
		})
	}
}

func TestInputMarshall(t *testing.T) {
	enc := zapcore.NewMapObjectEncoder()
	inp := &input{
		Message: &Message{},
	}
	require.NoError(t, inp.MarshalLogObject(enc))
}

func TestOutputMarshall(t *testing.T) {
	enc := zapcore.NewMapObjectEncoder()
	coin := true
	out := &output{
		coin:    &coin,
		result:  []types.ProposalID{{}},
		message: &Message{},
	}
	require.NoError(t, out.MarshalLogObject(enc))
}
