package hare4

import (
	"bytes"
	"fmt"
	"slices"
	"sync"

	"go.uber.org/zap/zapcore"
	"golang.org/x/exp/maps"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/malfeasance/wire"
)

type grade uint8

const (
	grade0 grade = iota
	grade1
	grade2
	grade3
	grade4
	grade5
)

func isSubset(s, v []types.ProposalID) bool {
	set := map[types.ProposalID]struct{}{}
	for _, id := range v {
		set[id] = struct{}{}
	}
	for _, id := range s {
		if _, exist := set[id]; !exist {
			return false
		}
	}
	return true
}

func toHash(proposals []types.ProposalID) types.Hash32 {
	return types.CalcProposalHash32Presorted(proposals, nil)
}

type messageKey struct {
	IterRound
	Sender types.NodeID
}

type input struct {
	*Message
	atxgrade  grade
	malicious bool
	msgHash   types.Hash32
}

func (i *input) MarshalLogObject(encoder zapcore.ObjectEncoder) error {
	if i.Message != nil {
		i.Message.MarshalLogObject(encoder)
	}
	encoder.AddUint8("atxgrade", uint8(i.atxgrade))
	encoder.AddBool("malicious", i.malicious)
	encoder.AddString("hash", i.msgHash.ShortString())
	return nil
}

type output struct {
	coin       *bool              // set based on preround messages right after preround completes in 0 iter
	result     []types.ProposalID // set based on notify messages at the start of next iter
	terminated bool               // protocol participates in one more iteration after outputing result
	message    *Message
}

func (o *output) MarshalLogObject(encoder zapcore.ObjectEncoder) error {
	encoder.AddBool("terminated", o.terminated)
	if o.coin != nil {
		encoder.AddBool("coin", *o.coin)
	}
	if o.result != nil {
		encoder.AddArray("result", zapcore.ArrayMarshalerFunc(func(encoder zapcore.ArrayEncoder) error {
			for _, id := range o.result {
				encoder.AppendString(types.Hash20(id).ShortString())
			}
			return nil
		}))
	}
	if o.message != nil {
		encoder.AddObject("msg", o.message)
	}
	return nil
}

func newProtocol(threshold uint16) *protocol {
	return &protocol{
		validProposals: map[types.Hash32][]types.ProposalID{},
		gossip:         gossip{threshold: threshold, state: map[messageKey]*gossipInput{}},
	}
}

type protocol struct {
	mu sync.Mutex
	IterRound
	coinout        bool
	coin           *types.VrfSignature // smallest vrf from preround messages. not a part of paper
	initial        []types.ProposalID  // Si
	result         *types.Hash32       // set after waiting for notify messages. Case 1
	locked         *types.Hash32       // Li
	hardLocked     bool
	validProposals map[types.Hash32][]types.ProposalID // Ti
	gossip         gossip
}

func (p *protocol) OnInitial(proposals []types.ProposalID) {
	slices.SortFunc(proposals, func(i, j types.ProposalID) int { return bytes.Compare(i[:], j[:]) })
	p.mu.Lock()
	defer p.mu.Unlock()
	p.initial = proposals
	fmt.Println("protocol on initial proposals", len(p.initial))
}

func (p *protocol) OnInput(msg *input) (bool, *wire.HareProof) {
	p.mu.Lock()
	defer p.mu.Unlock()

	gossip, equivocation := p.gossip.receive(p.IterRound, msg)
	if !gossip {
		return false, equivocation
	}
	if msg.Round == preround &&
		(p.coin == nil || (p.coin != nil && msg.Eligibility.Proof.Cmp(p.coin) == -1)) {
		p.coin = &msg.Eligibility.Proof
	}
	return gossip, equivocation
}

func (p *protocol) thresholdProposals(ir IterRound, grade grade) (*types.Hash32, []types.ProposalID) {
	for _, ref := range p.gossip.thresholdGossipRef(ir, grade) {
		valid, exist := p.validProposals[ref]
		if exist {
			return &ref, valid
		}
	}
	return nil, nil
}

func (p *protocol) commitExists(iter uint8, match types.Hash32, grade grade) bool {
	for _, ref := range p.gossip.thresholdGossipRef(IterRound{Iter: iter, Round: commit}, grade) {
		if ref == match {
			return true
		}
	}
	return false
}

func (p *protocol) execution(out *output) {
	// 4.3 Protocol Execution
	switch p.Round {
	case preround:
		out.message = &Message{Body: Body{
			IterRound: p.IterRound,
			Value:     Value{Proposals: p.initial},
		}}
	case hardlock:
		if p.Iter > 0 {
			if p.result != nil {
				out.terminated = true
			}
			ref, values := p.thresholdProposals(IterRound{Iter: p.Iter - 1, Round: notify}, grade5)
			if ref != nil && p.result == nil {
				p.result = ref
				out.result = values
				if values == nil {
					// receiver expects non-nil result
					out.result = []types.ProposalID{}
				}
			}
			if ref, _ := p.thresholdProposals(IterRound{Iter: p.Iter - 1, Round: commit}, grade4); ref != nil {
				p.locked = ref
				p.hardLocked = true
			} else {
				p.locked = nil
				p.hardLocked = false
			}
		}
	case softlock:
		if p.Iter > 0 && !p.hardLocked {
			if ref, _ := p.thresholdProposals(IterRound{Iter: p.Iter - 1, Round: commit}, grade3); ref != nil {
				p.locked = ref
			} else {
				p.locked = nil
			}
		}
	case propose:
		values := p.gossip.thresholdGossip(IterRound{Round: preround}, grade4)
		if p.Iter > 0 {
			ref, overwrite := p.thresholdProposals(IterRound{Iter: p.Iter - 1, Round: commit}, grade2)
			if ref != nil {
				values = overwrite
			}
		}
		out.message = &Message{Body: Body{
			IterRound: p.IterRound,
			Value:     Value{Proposals: values},
		}}
	case commit:
		// condition (d) is realized by ordering proposals by vrf
		proposed := p.gossip.gradecast(IterRound{Iter: p.Iter, Round: propose})
		g2values := p.gossip.thresholdGossip(IterRound{Round: preround}, grade2)
		for _, graded := range proposed {
			// condition (a) and (b)
			// grade0 proposals are not added to the set
			if !isSubset(graded.values, g2values) {
				continue
			}
			p.validProposals[toHash(graded.values)] = graded.values
		}
		if p.hardLocked && p.locked != nil {
			out.message = &Message{Body: Body{
				IterRound: p.IterRound,
				Value:     Value{Reference: p.locked},
			}}
		} else {
			g3values := p.gossip.thresholdGossip(IterRound{Round: preround}, grade3)
			g5values := p.gossip.thresholdGossip(IterRound{Round: preround}, grade5)
			for _, graded := range proposed {
				id := toHash(graded.values)
				// condition (c)
				if _, exist := p.validProposals[id]; !exist {
					continue
				}
				// condition (e)
				if graded.grade != grade2 {
					continue
				}
				// condition (f)
				if !isSubset(graded.values, g3values) {
					continue
				}
				// condition (g)
				if !isSubset(g5values, graded.values) && !p.commitExists(p.Iter-1, id, grade1) {
					continue
				}
				// condition (h)
				if p.locked != nil && *p.locked != id {
					continue
				}
				out.message = &Message{Body: Body{
					IterRound: p.IterRound,
					Value:     Value{Reference: &id},
				}}
				break
			}
		}
	case notify:
		ref := p.result
		if ref == nil {
			ref, _ = p.thresholdProposals(IterRound{Iter: p.Iter, Round: commit}, grade5)
		}
		if ref != nil {
			out.message = &Message{Body: Body{
				IterRound: p.IterRound,
				Value:     Value{Reference: ref},
			}}
		}
	}
}

func (p *protocol) Next() output {
	p.mu.Lock()
	defer p.mu.Unlock()

	out := output{}
	p.execution(&out)
	if p.Round >= softlock && p.coin != nil && !p.coinout {
		coin := p.coin.LSB() != 0
		out.coin = &coin
		p.coinout = true
	}
	if p.Round == preround && p.Iter == 0 {
		// skips hardlock unlike softlock in the paper.
		// this makes no practical difference from correctness.
		// but allows to simplify assignment in validValues
		p.Round = softlock
	} else if p.Round == notify {
		p.Round = hardlock
		p.Iter++
	} else {
		p.Round++
	}
	return out
}

func (p *protocol) Stats() *stats {
	p.mu.Lock()
	defer p.mu.Unlock()
	s := &stats{
		iter:      p.Iter - 1,
		threshold: p.gossip.threshold,
	}
	// preround messages that are received after the very first iteration
	// has no impact on protocol
	if s.iter == 0 {
		for grade := grade1; grade <= grade5; grade++ {
			s.preround = append(s.preround, preroundStats{
				grade: grade,
				tallies: maps.Values(
					thresholdTallies(p.gossip.state, IterRound{Round: preround}, grade, tallyProposals),
				),
			})
		}
	}
	proposals := p.gossip.gradecast(IterRound{Iter: p.Iter - 1, Round: propose})
	for _, graded := range proposals {
		s.propose = append(s.propose, proposeStats{
			grade:     graded.grade,
			ref:       toHash(graded.values),
			proposals: graded.values,
		})
	}
	// stats are collected at the start of current iteration (p.Iter)
	// we expect 2 network delays to pass since commit messages were broadcasted
	for grade := grade4; grade <= grade5; grade++ {
		s.commit = append(s.commit, commitStats{
			grade: grade,
			tallies: maps.Values(
				thresholdTallies(p.gossip.state, IterRound{Iter: p.Iter - 1, Round: commit}, grade, tallyRefs),
			),
		})
	}
	// we are not interested in any other grade for notify message as they have no impact on protocol execution
	s.notify = append(s.notify, notifyStats{
		grade: grade5,
		tallies: maps.Values(
			thresholdTallies(p.gossip.state, IterRound{Iter: p.Iter - 1, Round: notify}, grade5, tallyRefs),
		),
	})
	return s
}

type gossipInput struct {
	*input
	received      IterRound
	otherReceived *IterRound
}

// Protocol 1. graded-gossip. page 10.
type gossip struct {
	threshold uint16
	state     map[messageKey]*gossipInput
}

func (g *gossip) receive(current IterRound, input *input) (bool, *wire.HareProof) {
	// Case 1: will be discarded earlier
	other, exist := g.state[input.key()]
	if exist {
		if other.msgHash != input.msgHash && !other.malicious {
			// Protocol 3. thresh-gossip. keep one with the maximal grade.
			if input.atxgrade > other.atxgrade {
				input.malicious = true
				g.state[input.key()] = &gossipInput{
					input:         input,
					received:      current,
					otherReceived: &other.received,
				}
			} else {
				// Case 3
				other.malicious = true
				other.otherReceived = &current
			}
			return true, &wire.HareProof{Messages: [2]wire.HareProofMsg{
				other.ToMalfeasanceProof(), input.ToMalfeasanceProof(),
			}}
		}
		// Case 2. but also we filter duplicates from p2p layer here
		return false, nil
	}
	// Case 4
	g.state[input.key()] = &gossipInput{input: input, received: current}
	return true, nil
}

type gset struct {
	values   []types.ProposalID
	grade    grade
	smallest types.VrfSignature
}

// Protocol 2. gradecast. page 13.
func (g *gossip) gradecast(target IterRound) []gset {
	// unlike paper we use 5-graded gossip for gradecast as well
	var rst []gset
	for key, value := range g.state {
		if key.IterRound == target && (!value.malicious || value.otherReceived != nil) {
			if value.atxgrade == grade5 && value.received.Delay(target) <= 1 &&
				// 2 (a)
				(value.otherReceived == nil || value.otherReceived.Delay(target) > 3) {
				// 2 (b)
				rst = append(rst, gset{
					grade:    grade2,
					values:   value.Value.Proposals,
					smallest: value.Eligibility.Proof,
				})
			} else if value.atxgrade >= grade4 && value.received.Delay(target) <= 2 &&
				// 3 (a)
				(value.otherReceived == nil || value.otherReceived.Delay(target) > 2) {
				// 3 (b)
				rst = append(rst, gset{
					grade:    grade1,
					values:   value.Value.Proposals,
					smallest: value.Eligibility.Proof,
				})
			}
		}
	}
	// hare expects to receive multiple proposals. expected number of leaders is set to 5.
	// we need to choose the same one for commit across the cluster.
	// we do that by ordering them by vrf value, and picking one that passes other checks (see commit in execution).
	// in hare3 paper look for p-Weak leader election property.
	slices.SortFunc(rst, func(i, j gset) int {
		return i.smallest.Cmp(&j.smallest)
	})
	return rst
}

func tallyProposals(all map[types.ProposalID]proposalTally, inp *gossipInput) {
	for _, id := range inp.Value.Proposals {
		tally, exist := all[id]
		if !exist {
			tally = proposalTally{id: id}
		}
		tally.total += inp.Eligibility.Count
		if !inp.malicious {
			tally.valid += inp.Eligibility.Count
		}
		all[id] = tally
	}
}

// Protocol 3. thresh-gossip. Page 15.
// output returns union of sorted proposals received
// in the given round with minimal specified grade.
func (g *gossip) thresholdGossip(filter IterRound, grade grade) []types.ProposalID {
	rst := thresholdGossip(thresholdTallies(g.state, filter, grade, tallyProposals), g.threshold)
	slices.SortFunc(rst, func(i, j types.ProposalID) int {
		return bytes.Compare(i.Bytes(), j.Bytes())
	})
	return rst
}

func tallyRefs(all map[types.Hash32]refTally, inp *gossipInput) {
	tally, exist := all[*inp.Value.Reference]
	if !exist {
		tally = refTally{id: *inp.Value.Reference}
	}
	tally.total += inp.Eligibility.Count
	if !inp.malicious {
		tally.valid += inp.Eligibility.Count
	}
	all[*inp.Value.Reference] = tally
}

// thresholdGossipRef returns all references to proposals in the given round with minimal grade.
func (g *gossip) thresholdGossipRef(filter IterRound, grade grade) []types.Hash32 {
	return thresholdGossip(thresholdTallies(g.state, filter, grade, tallyRefs), g.threshold)
}

func thresholdGossip[T interface {
	comparable
	fmt.Stringer
}](
	tallies map[T]tallyStats[T], threshold uint16,
) []T {
	rst := []T{}
	for _, item := range tallies {
		// valid > 0 and total >= f
		// atleast one non-equivocating vote and crossed committee/2 + 1
		if item.total >= threshold && item.valid > 0 {
			rst = append(rst, item.id)
		}
	}
	return rst
}

func thresholdTallies[T interface {
	comparable
	fmt.Stringer
}](
	state map[messageKey]*gossipInput,
	filter IterRound,
	msgGrade grade,
	tally func(tally map[T]tallyStats[T], inp *gossipInput),
) map[T]tallyStats[T] {
	all := map[T]tallyStats[T]{}
	min := grade5
	// pick min atx grade from non equivocating identity.
	for key, value := range state {
		if key.IterRound == filter && value.atxgrade < min && !value.malicious &&
			value.received.Grade(filter) >= msgGrade {
			min = value.atxgrade
		}
	}
	// tally votes for valid and malicious messages
	for key, value := range state {
		if key.IterRound == filter && value.atxgrade >= min && value.received.Grade(filter) >= msgGrade {
			tally(all, value)
		}
	}
	return all
}

type preroundStats struct {
	grade   grade
	tallies []proposalTally
}

func (s *preroundStats) MarshalLogObject(encoder zapcore.ObjectEncoder) error {
	encoder.AddUint16("grade", uint16(s.grade))
	encoder.AddArray("tallies", zapcore.ArrayMarshalerFunc(func(enc zapcore.ArrayEncoder) error {
		for _, tally := range s.tallies {
			enc.AppendObject(&tally)
		}
		return nil
	}))
	return nil
}

type tallyStats[T fmt.Stringer] struct {
	id    T
	total uint16
	valid uint16
}

func (s *tallyStats[T]) MarshalLogObject(encoder zapcore.ObjectEncoder) error {
	encoder.AddUint16("total", s.total)
	encoder.AddUint16("valid", s.valid)
	encoder.AddString("id", s.id.String())
	return nil
}

type (
	proposalTally = tallyStats[types.ProposalID]
	refTally      = tallyStats[types.Hash32]
)

type proposeStats struct {
	grade     grade
	ref       types.Hash32
	proposals []types.ProposalID
}

func (s *proposeStats) MarshalLogObject(encoder zapcore.ObjectEncoder) error {
	encoder.AddString("ref", s.ref.ShortString())
	encoder.AddUint16("grade", uint16(s.grade))
	encoder.AddArray("proposals", zapcore.ArrayMarshalerFunc(func(encoder zapcore.ArrayEncoder) error {
		for _, id := range s.proposals {
			encoder.AppendString(id.String())
		}
		return nil
	}))
	return nil
}

type commitStats struct {
	grade   grade
	tallies []refTally
}

func (s *commitStats) MarshalLogObject(encoder zapcore.ObjectEncoder) error {
	encoder.AddUint16("grade", uint16(s.grade))
	encoder.AddArray("tallies", zapcore.ArrayMarshalerFunc(func(encoder zapcore.ArrayEncoder) error {
		for _, tally := range s.tallies {
			encoder.AppendObject(&tally)
		}
		return nil
	}))
	return nil
}

type notifyStats struct {
	grade   grade
	tallies []refTally
}

func (n *notifyStats) MarshalLogObject(encoder zapcore.ObjectEncoder) error {
	encoder.AddUint16("grade", uint16(n.grade))
	encoder.AddArray("tallies", zapcore.ArrayMarshalerFunc(func(encoder zapcore.ArrayEncoder) error {
		for _, tally := range n.tallies {
			encoder.AppendObject(&tally)
		}
		return nil
	}))
	return nil
}

type stats struct {
	iter      uint8
	threshold uint16
	preround  []preroundStats
	propose   []proposeStats
	commit    []commitStats
	notify    []notifyStats
}

func (s *stats) MarshalLogObject(encoder zapcore.ObjectEncoder) error {
	encoder.AddUint8("iter", s.iter)
	encoder.AddUint16("threshold", s.threshold)
	encoder.AddArray("preround", zapcore.ArrayMarshalerFunc(func(encoder zapcore.ArrayEncoder) error {
		for _, stat := range s.preround {
			encoder.AppendObject(&stat)
		}
		return nil
	}))
	encoder.AddArray("propose", zapcore.ArrayMarshalerFunc(func(encoder zapcore.ArrayEncoder) error {
		for _, stat := range s.propose {
			encoder.AppendObject(&stat)
		}
		return nil
	}))
	encoder.AddArray("commit", zapcore.ArrayMarshalerFunc(func(encoder zapcore.ArrayEncoder) error {
		for _, stat := range s.commit {
			encoder.AppendObject(&stat)
		}
		return nil
	}))
	encoder.AddArray("notify", zapcore.ArrayMarshalerFunc(func(encoder zapcore.ArrayEncoder) error {
		for _, stat := range s.notify {
			encoder.AppendObject(&stat)
		}
		return nil
	}))
	return nil
}
