package hare3

import (
	"sort"
	"sync"

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
	validValues    [grade5 + 1][]types.ProposalID      // Vi
	gossip         gossip
}

func (p *protocol) OnInitial(proposals []types.ProposalID) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.initial = proposals
}

func (p *protocol) OnInput(msg *input) (bool, *types.HareProof) {
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

func (p *protocol) thresholdProposals(ir IterRound) (*types.Hash32, []types.ProposalID) {
	for _, ref := range p.gossip.thresholdGossipRef(ir) {
		valid, exist := p.validProposals[ref]
		if exist {
			return &ref, valid
		}
	}
	return nil, nil
}

func (p *protocol) commitExists(iter uint8, match types.Hash32) bool {
	for _, ref := range p.gossip.thresholdGossipRef(IterRound{Iter: iter, Round: commit}) {
		if ref == match {
			return true
		}
	}
	return false
}

func (p *protocol) execution(out *output) {
	// 4.3 Protocol Execution
	if p.Iter == 0 && p.Round >= softlock && p.Round <= wait2 {
		// -1 - skipped hardlock round in iter 0
		// -1 - implementation rounds starts from 0
		g := grade5 - grade(p.Round-2)
		p.validValues[g] = p.gossip.thresholdGossip(IterRound{Round: preround})
	}
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
			ref, values := p.thresholdProposals(IterRound{Iter: p.Iter - 1, Round: notify})
			if ref != nil && p.result == nil {
				p.result = ref
				out.result = values
				if values == nil {
					// receiver expects non-nil result
					out.result = []types.ProposalID{}
				}
			}
			if ref, _ := p.thresholdProposals(IterRound{Iter: p.Iter - 1, Round: commit}); ref != nil {
				p.locked = ref
				p.hardLocked = true
			} else {
				p.locked = nil
				p.hardLocked = false
			}
		}
	case softlock:
		if p.Iter > 0 && !p.hardLocked {
			if ref, _ := p.thresholdProposals(IterRound{Iter: p.Iter - 1, Round: commit}); ref != nil {
				p.locked = ref
			} else {
				p.locked = nil
			}
		}
	case propose:
		values := p.validValues[grade4]
		if p.Iter > 0 {
			ref, overwrite := p.thresholdProposals(IterRound{Iter: p.Iter - 1, Round: commit})
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
		for _, graded := range proposed {
			// condition (a) and (b)
			if graded.grade < grade1 || !isSubset(graded.values, p.validValues[grade2]) {
				continue
			}
			p.validProposals[toHash(graded.values)] = graded.values
		}
		var ref *types.Hash32
		if p.hardLocked && p.locked != nil {
			ref = p.locked
		} else {
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
				if !isSubset(graded.values, p.validValues[grade3]) {
					continue
				}
				// condition (g)
				if !isSubset(p.validValues[grade5], graded.values) &&
					!p.commitExists(p.Iter-1, id) {
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
	case notify:
		ref := p.result
		if ref == nil {
			ref, _ = p.thresholdProposals(IterRound{Iter: p.Iter, Round: commit})
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

func (g *gossip) receive(current IterRound, input *input) (bool, *types.HareProof) {
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
			return true, &types.HareProof{Messages: [2]types.HareProofMsg{
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
			g := grade0
			if value.atxgrade == grade5 && value.received.Delay(target) <= 1 &&
				// 2 (a)
				(value.otherReceived == nil || value.otherReceived.Delay(target) > 3) {
				// 2 (b)
				g = grade2
			} else if value.atxgrade >= grade4 && value.received.Delay(target) <= 2 &&
				// 3 (a)
				(value.otherReceived == nil || value.otherReceived.Delay(target) > 2) {
				// 3 (b)
				g = grade1
			}
			if g > grade0 {
				rst = append(rst, gset{
					grade:    g,
					values:   value.Value.Proposals,
					smallest: value.Eligibility.Proof,
				})
			}
		}
	}
	// it satisfies p-Weak leader election
	// inconsistent order of proposals may cause participants to commit on different proposals
	sort.Slice(rst, func(i, j int) bool {
		return rst[i].smallest.Cmp(&rst[j].smallest) == -1
	})
	return rst
}

// Protocol 3. thresh-gossip. Page 15.
// output returns union of sorted proposals received
// in the given round with minimal specified grade.
func (g *gossip) thresholdGossip(filter IterRound) []types.ProposalID {
	rst := thresholdGossip(g.state, g.threshold, filter,
		func(total map[types.ProposalID]uint16, valid map[types.ProposalID]struct{}, inp *gossipInput) {
			for _, id := range inp.Value.Proposals {
				total[id] += inp.Eligibility.Count
				if !inp.malicious {
					valid[id] = struct{}{}
				}
			}
		})
	return types.SortProposalIDs(rst)
}

// thresholdGossipRef returns all references to proposals in the given round with minimal grade.
func (g *gossip) thresholdGossipRef(filter IterRound) []types.Hash32 {
	return thresholdGossip(g.state, g.threshold, filter,
		func(total map[types.Hash32]uint16, valid map[types.Hash32]struct{}, inp *gossipInput) {
			total[*inp.Value.Reference] += inp.Eligibility.Count
			if !inp.malicious {
				valid[*inp.Value.Reference] = struct{}{}
			}
		})
}

func thresholdGossip[T comparable](
	state map[messageKey]*gossipInput, threshold uint16, filter IterRound,
	tally func(all map[T]uint16, good map[T]struct{}, inp *gossipInput),
) []T {
	total := map[T]uint16{}
	valid := map[T]struct{}{}
	min := grade5
	// pick min atx grade from non equivocating identity.
	for key, value := range state {
		if key.IterRound == filter && value.atxgrade < min && !value.malicious {
			min = value.atxgrade
		}
	}
	// tally votes for valid and malicious messages
	for key, value := range state {
		if key.IterRound == filter && value.atxgrade >= min {
			tally(total, valid, value)
		}
	}
	rst := []T{}
	// valid > 0 and total >= f
	// atleast one non-equivocating vote and crossed committee/2 + 1
	for id := range valid {
		if total[id] >= threshold {
			rst = append(rst, id)
		}
	}
	return rst
}
