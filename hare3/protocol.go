package hare3

import (
	"sort"

	"go.uber.org/zap/zapcore"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/log"
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

type input struct {
	*Message
	grade     grade
	malicious bool
	msgHash   types.Hash32
}

func (i *input) MarshalLogObject(encoder zapcore.ObjectEncoder) error {
	encoder.AddObject("msg", i.Message)
	encoder.AddUint8("grade", uint8(i.grade))
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
		encoder.AddArray("result", zapcore.ArrayMarshalerFunc(func(encoder log.ArrayEncoder) error {
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

type validProposal struct {
	iter      uint8
	proposals []types.ProposalID
}

type gradedProposals struct {
	values [6][]types.ProposalID // the length is set to 6 so that 5 is a valid index
}

func (g *gradedProposals) set(gr grade, values []types.ProposalID) {
	g.values[gr] = values
}

func (g *gradedProposals) get(gr grade) []types.ProposalID {
	return g.values[gr]
}

func newProtocol(initial []types.ProposalID, threshold uint16) *protocol {
	return &protocol{
		initial:        initial,
		validProposals: map[types.Hash32]validProposal{},
		gradedGossip:   gradedGossip{state: map[messageKey]*input{}},
		gradecast:      gradecast{state: map[messageKey]*gradecasted{}},
		thresholdGossip: thresholdGossip{
			threshold: threshold,
			state:     map[messageKey]*votes{},
		},
	}
}

type protocol struct {
	IterRound
	coinout         bool
	coin            *types.VrfSignature // smallest vrf from preround messages. not a part of paper
	initial         []types.ProposalID  // Si
	result          *types.Hash32       // set after Round 6. Case 1
	locked          *types.Hash32       // Li
	hardLocked      bool
	validProposals  map[types.Hash32]validProposal // Vi
	gradedProposals gradedProposals                // valid values in 4.3
	gradedGossip    gradedGossip
	gradecast       gradecast
	thresholdGossip thresholdGossip
}

func (p *protocol) onMessage(msg *input) (bool, *types.HareProof) {
	gossip, equivocation := p.gradedGossip.receive(msg)
	if !gossip {
		return false, equivocation
	}
	// gradecast and thresholdGossip should never be called with non-equivocating duplicates
	if msg.Round == propose {
		p.gradecast.add(msg.grade, p.IterRound, msg)
	} else {
		p.thresholdGossip.add(msg.grade, msg)
	}
	if msg.Round == preround &&
		(p.coin == nil || (p.coin != nil && msg.Eligibility.Proof.Cmp(p.coin) == -1)) {
		p.coin = &msg.Eligibility.Proof
	}
	return gossip, equivocation
}

func (p *protocol) thresholdProposals(ir IterRound, grade grade) (*types.Hash32, []types.ProposalID) {
	for _, ref := range p.thresholdGossip.outputref(ir, grade) {
		valid, exist := p.validProposals[ref]
		if exist {
			return &ref, valid.proposals
		}
	}
	return nil, nil
}

func (p *protocol) commitExists(iter uint8, grade grade, match types.Hash32) bool {
	for _, ref := range p.thresholdGossip.outputref(IterRound{Iter: iter, Round: commit}, grade) {
		if ref == match {
			return true
		}
	}
	return false
}

func (p *protocol) execution(out *output, active bool) {
	// code below aims to look similar to 4.3 Protocol Execution
	if p.Iter == 0 && p.Round >= softlock && p.Round <= wait2 {
		// -1 - skipped hardlock round in iter 0
		// -1 - implementation rounds starts from 0
		g := grade5 - grade(p.Round-2)
		p.gradedProposals.set(g, p.thresholdGossip.output(IterRound{Round: preround}, g))
	}
	if p.Round == preround && active {
		out.message = &Message{Body: Body{
			IterRound: p.IterRound,
			Value:     Value{Proposals: p.initial},
		}}
	} else if p.Round == hardlock && p.Iter > 0 {
		if p.result != nil {
			out.terminated = true
		}
		ref, values := p.thresholdProposals(IterRound{Iter: p.Iter - 1, Round: notify}, grade5)
		if ref != nil && p.result == nil {
			p.result = ref
			out.result = values
		}
		if ref, _ := p.thresholdProposals(IterRound{Iter: p.Iter - 1, Round: commit}, grade4); ref != nil {
			p.locked = ref
			p.hardLocked = true
		} else {
			p.locked = nil
			p.hardLocked = false
		}
	} else if p.Round == softlock && p.Iter > 0 && !p.hardLocked {
		if ref, _ := p.thresholdProposals(IterRound{Iter: p.Iter - 1, Round: commit}, grade3); ref != nil {
			p.locked = ref
		} else {
			p.locked = nil
		}
	} else if p.Round == propose && active {
		values := p.gradedProposals.get(grade4)
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
	} else if p.Round == commit {
		proposed := p.gradecast.output(IterRound{Iter: p.Iter, Round: propose})
		for _, graded := range proposed {
			// condition (a) and (b)
			if graded.grade < grade1 || !isSubset(graded.values, p.gradedProposals.get(grade2)) {
				continue
			}
			p.validProposals[toHash(graded.values)] = validProposal{
				iter:      p.Iter,
				proposals: graded.values,
			}
		}
		if active {
			var ref *types.Hash32
			if p.hardLocked && p.locked != nil {
				ref = p.locked
			} else {
				for _, graded := range proposed {
					id := toHash(graded.values)
					// condition (c)
					if proposal, exist := p.validProposals[id]; !exist || proposal.iter > p.Iter {
						continue
					}
					// TODO(dshulyak) doublecheck if there are nuances about (d)
					// condition (d) IsLeader is implicit, propose won't pass eligibility check
					// condition (e)
					if graded.grade != grade2 {
						continue
					}
					// condition (f)
					if !isSubset(graded.values, p.gradedProposals.get(grade3)) {
						continue
					}
					// condition (g)
					// TODO(dshulyak) if no proposals with grade5 1st condition is noop
					if !isSubset(p.gradedProposals.get(grade5), graded.values) &&
						!p.commitExists(p.Iter-1, grade1, id) {
						continue
					}
					// condition (h)
					if p.locked != nil && *p.locked != id {
						continue
					}
					ref = &id
					break
				}
			}
			if ref != nil {
				out.message = &Message{Body: Body{
					IterRound: p.IterRound,
					Value:     Value{Reference: ref},
				}}
			}
		}
	} else if p.Round == notify && active {
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

func (p *protocol) next(active bool) output {
	out := output{}
	p.execution(&out, active)
	if p.Round >= softlock && p.coin != nil && !p.coinout {
		coin := p.coin.LSB() != 0
		out.coin = &coin
		p.coinout = true
	}
	if p.Round == preround && p.Iter == 0 {
		p.Round = softlock // skips hardlock
	} else if p.Round == notify {
		p.Round = hardlock
		p.Iter++
	} else {
		p.Round++
	}
	return out
}

// Protocol 1. graded-gossip. page 10
type gradedGossip struct {
	state map[messageKey]*input
}

func (g *gradedGossip) receive(input *input) (bool, *types.HareProof) {
	// Case 1: will not be discarded earlier
	other, exist := g.state[input.key()]
	if exist {
		if other.msgHash != input.msgHash && !other.malicious {
			// Case 3
			other.malicious = true
			return true, &types.HareProof{Messages: [2]types.HareProofMsg{
				other.ToMalfeasenceProof(), input.ToMalfeasenceProof(),
			}}
		}
		// Case 2. but also we filter duplicates from p2p layer here
		return false, nil
	}
	// Case 4
	g.state[input.key()] = input
	return true, nil
}

// Protocol 2. gradecast. page 13
type gradecast struct {
	state map[messageKey]*gradecasted
}

type gradecasted struct {
	grade    grade
	received IterRound
	// set if another was received from graded gossip
	otherReceived *IterRound
	malicious     bool
	values        []types.ProposalID
	vrf           types.VrfSignature
}

func (g *gradecast) add(grade grade, current IterRound, msg *input) {
	if stored, exist := g.state[msg.key()]; !exist {
		g.state[msg.key()] = &gradecasted{
			grade:     grade,
			received:  current,
			malicious: msg.malicious,
			values:    msg.Value.Proposals,
			vrf:       msg.Eligibility.Proof,
		}
	} else {
		stored.otherReceived = &current
	}
}

type gset struct {
	values   []types.ProposalID
	grade    grade
	smallest types.VrfSignature
}

func (g *gradecast) output(target IterRound) []gset {
	var rst []gset
	for key, value := range g.state {
		if key.IterRound == target && !value.malicious {
			g := grade0
			if value.grade == grade3 && value.received.Delay(target) <= 1 &&
				// 2 (a)
				(value.otherReceived == nil || value.otherReceived.Delay(target) > 3) {
				// 2 (b)
				g = grade2
			} else if value.grade >= grade2 && value.received.Delay(target) <= 2 &&
				// 3 (a)
				(value.otherReceived == nil || value.otherReceived.Delay(target) > 2) {
				// 3 (b)
				g = grade1
			}
			if g > grade0 {
				rst = append(rst, gset{
					grade:    g,
					values:   value.values,
					smallest: value.vrf,
				})
			}
		}
	}
	// TODO(dshulyak) i think it satisfies p-Weak leader election
	// inconsistent order of proposals may cause participants to commit
	// on different proposals
	sort.Slice(rst, func(i, j int) bool {
		return rst[i].smallest.Cmp(&rst[j].smallest) == -1
	})
	return rst
}

type votes struct {
	grade         grade
	eligibilities uint16
	malicious     bool
	value         Value
}

// Protocol 3. thresh-gossip. Page 15
type thresholdGossip struct {
	threshold uint16
	state     map[messageKey]*votes
}

func (t *thresholdGossip) add(grade grade, input *input) {
	if stored, exist := t.state[input.key()]; !exist {
		t.state[input.key()] = &votes{
			grade:         grade,
			eligibilities: input.Eligibility.Count,
			malicious:     input.malicious,
			value:         input.Value,
		}
	} else {
		stored.malicious = true
	}
}

// output returns union of sorted proposals received
// in the given round with minimal specified grade.
func (t *thresholdGossip) output(filter IterRound, grade grade) []types.ProposalID {
	all := map[types.ProposalID]uint16{}
	good := map[types.ProposalID]struct{}{}
	for key, value := range t.state {
		if key.IterRound == filter && value.grade >= grade {
			for _, id := range value.value.Proposals {
				all[id] += value.eligibilities
				if !value.malicious {
					good[id] = struct{}{}
				}
			}
		}
	}
	rst := []types.ProposalID{}
	for id := range good {
		if all[id] >= t.threshold {
			rst = append(rst, id)
		}
	}
	types.SortProposalIDs(rst)
	return rst
}

// filterred returns all references to proposals in the given round with minimal grade.
func (t *thresholdGossip) outputref(filter IterRound, grade grade) []types.Hash32 {
	all := map[types.Hash32]uint16{}
	good := map[types.Hash32]struct{}{}
	for key, value := range t.state {
		if key.IterRound == filter && value.grade >= grade {
			// nil should not be valid in this codepath
			// this is enforced by correctly decoded messages
			id := *value.value.Reference
			all[id] += value.eligibilities
			if !value.malicious {
				good[id] = struct{}{}
			}
		}
	}
	var rst []types.Hash32
	for id := range good {
		if all[id] >= t.threshold {
			rst = append(rst, id)
		}
	}
	return rst
}
