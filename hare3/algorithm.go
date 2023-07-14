// package hare3 has an improved version of hare that solves the problem of
// excessive bandwidth usage and makes allowance for having only partial
// agreement over the set of participants.
//
// The design of this package strives to remove any extraneous information from
// the protocol's implementation so as to simplify the logic, and reduce the
// dependencies of this package. To this end the protocol does not contain or
// handle any errors, all error generating code has been pushed outside of the
// protocol.
//
// It also strives to reduce nesting/deep call stacks, so instead of nesting
// objects inside each other we compose their functionality, this makes testing
// individual components easier because they do less and it removes the need to
// mock in many cases, it also aids at keeping the code flexible, since, for
// example, if we want to change where the output of threshGossip goes we don't
// need to modify threshGossip or re-implement gradedGossip.
//
// E.G.
// # instead of
//
//	func threshGossip(x) {
//	   ...
//	   gradedGossip(y)
//	   ...
//	}
//
//	func gradedGossip(y) {
//	   ...
//	   networkGossip(z)
//	   ...
//	}
//
// # We do
//
// y := threshGossip(x)
// z := gradedGossip(y)
// networkGossip(z)
//
// Additionally there is no use of concurrency in this package, however Handler
// and Protocol share a mutex since they will be called from multiple
// go-routines and they share state.
package hare3

import (
	"crypto/sha256"
	"sync"
	"time"

	"golang.org/x/exp/slices"

	"github.com/spacemeshos/go-spacemesh/common/types"
)

type (
	MsgType            int
	GradedGossipResult uint8
)

const (
	Preround MsgType = -1
	HardLock MsgType = 0
	SoftLock MsgType = 1
	Propose  MsgType = 2

	Commit MsgType = 5
	Notify MsgType = 6

	SendValue GradedGossipResult = iota
	SendEquivocationProof
	DropMessage

	// d is the degree parameter of both the graded gossip and threshold gossip
	// protocols, it is highest grade that can be assigned, grades start at 0.
	// TODO see if I actually need this.
	d = 5
)

func (mt MsgType) Round() AbsRound {
	return AbsRound(mt)
}

// Ensure takes a nested map and ensures that there is a nested map entry for
// key k. It returns the nested map entry.
func Ensure[K1, K2 comparable, V any](m map[K1]map[K2]V, k K1) map[K2]V {
	entry, ok := m[k]
	if !ok {
		entry = make(map[K2]V)
		m[k] = entry
	}
	return entry
}

// EnsureValue takes a map and enures that there is a value for the given key.
func EnsureValue[K1 comparable, V any](m map[K1]V, k K1, constructor func() V) V {
	entry, ok := m[k]
	if !ok {
		entry = constructor()
		m[k] = entry
	}
	return entry
}

type LeaderChecker interface {
	IsLeader(vk types.NodeID, round AbsRound) bool
}

// gradeKey3 returns a grade from 0-3.
func gradeKey3(key types.NodeID) uint8 {
	return 3
}

// gradeKey5 returns a grade from 0-5.
func gradeKey5(key types.NodeID) uint8 {
	return 5
}

// RoundProvider provides a simple way to discover what is the current round.
type RoundProvider struct {
	layerTime     time.Time
	roundDuration time.Duration
}

func NewRoundProvider(layerTime time.Time, roundDuration time.Duration) *RoundProvider {
	return &RoundProvider{
		layerTime:     layerTime,
		roundDuration: roundDuration,
	}
}

func (rp *RoundProvider) CurrentRound() AbsRound {
	return AbsRound(time.Since(rp.layerTime) / rp.roundDuration)
}

// Handler performs message handling, there is a handler per instance of each
// hare protocol, and as such the handler does not need to be aware of the
// session id. It is also assumed that some higher level handler performs the
// actions of message decoding and signature verification and key grading,
// leaving this handler to handle the decoded message inputs.
type Handler struct {
	gg  GradedGossiper
	tgg ThresholdGradedGossiper
	gc  Gradecaster
	rp  *RoundProvider
	mu  *sync.Mutex
}

func NewHandler(gg GradedGossiper, tgg ThresholdGradedGossiper, gc Gradecaster, rp *RoundProvider) *Handler {
	return &Handler{
		gg:  gg,
		tgg: tgg,
		gc:  gc,
		rp:  rp,
		mu:  &sync.Mutex{},
	}
}

// HandleMsg handles an incoming message, it returns a boolean indicating
// whether the message should be regossipped to peers.
func (h *Handler) HandleMsg(hash types.Hash20, id types.NodeID, round AbsRound, values []types.Hash20) (bool, *types.Hash20) {
	h.mu.Lock()
	defer h.mu.Unlock()
	r := AbsRound(round)
	var g uint8
	switch r.Type() {
	case Propose:
		g = gradeKey3(id)
	default:
		g = gradeKey5(id)
	}

	var equivocationHash *types.Hash20
	// Nil values signifies a message from a known malicious actor, however we
	// still need to forward the first such message per identity to the network
	// though so that they also insert it into their protocols.
	result, eHash := h.gg.ReceiveMsg(hash, id, values, r)
	switch result {
	case DropMessage:
		// Indicates prior equivocation, drop the message.
		return false, nil
	case SendEquivocationProof:
		// Indicates new instance of equivocation notify gradecast or threshold
		// gossip with nil values.
		// Set the equivocationHash so that the caller can look up the message.
		equivocationHash = eHash
	}

	curr := h.rp.CurrentRound()
	// In the case of early messages we just set the curr round to be equal to
	// the message round. I.E. we assume they were delivered perfectly on time.
	if curr < round {
		curr = round
	}

	// Pass results to gradecast or threshold gossip.
	// Only proposals are gradecasted, all other message types are passed to
	// threshold gossip.
	if r.Type() == Propose {
		// Send to gradecast
		h.gc.ReceiveMsg(id, values, r, curr, g)
	} else {
		// Pass result to threshold gossip
		h.tgg.ReceiveMsg(id, values, r, curr, g)
	}
	return true, equivocationHash
}

func (h *Handler) Protocol(lc LeaderChecker, si []types.Hash20) *Protocol {
	return NewProtocol(h.tgg, h.gc, lc, h.mu, si)
}

type Protocol struct {
	// round encodes both iteration and round in a single value.
	round AbsRound
	// Each index "i" holds an indication of whether iteration "i" is hard locked
	hardLocked []bool
	// Each index "i" holds the locked value for iteration "i"
	Li []*types.Hash20
	// Each index "i" holds values considered valid up to round "i+1"
	// TODO convert this to an object that accepts grades, so that it can be more closely mapped to the paper
	Vi [][]types.Hash20
	// Each index "i" holds a map of sets of values from valid proposals
	// indexed by their hash received in iteration "i"
	Ti  []map[types.Hash20][]types.Hash20
	Si  []types.Hash20
	tgg ThresholdGradedGossiper
	gc  Gradecaster
	lc  LeaderChecker
	mu  *sync.Mutex
}

func NewProtocol(tgg ThresholdGradedGossiper, gc Gradecaster, lc LeaderChecker, protocolMu *sync.Mutex, si []types.Hash20) *Protocol {
	return &Protocol{
		round: AbsRound(-1),
		tgg:   tgg,
		gc:    gc,
		lc:    lc,
		mu:    protocolMu,
		Si:    si,
	}
}

func (p *Protocol) Round() AbsRound {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.round
}

func toHash(values []types.Hash20) types.Hash20 {
	h := sha256.New()
	for _, v := range values {
		h.Write(v[:])
	}
	var result types.Hash20
	copy(result[:], h.Sum(nil))
	return result
}

// Given a slice of candidate set hashes and a slice of sets of valid set
// hashes, one set per iteration, returns the first candidate that appears in
// the valid hashes along with it's set of any iteration before j.
func matchInPreviousIterations(candidates []types.Hash20, validSets []map[types.Hash20][]types.Hash20, j int8) (*types.Hash20, []types.Hash20) {
	for i := 0; i < int(j); i++ {
		validIteration := validSets[i]
		for _, v := range candidates {
			set, ok := validIteration[v]
			if ok {
				return &v, set
			}
		}
	}
	return nil, nil
}

// Given a slice of candidate set hashes and a slice of sets of valid set
// hashes, one per iteration, returns the first candidate that appears in the
// valid hashes, along with it's set of iteration j.
func matchInIteration(candidates []types.Hash20, validSets []map[types.Hash20][]types.Hash20, j int8) (*types.Hash20, []types.Hash20) {
	validJ := validSets[j]
	for _, v := range candidates {
		set, ok := validJ[v]
		if ok {
			return &v, set
		}
	}
	return nil, nil
}

func isSubset(subset, superset []types.Hash20) bool {
	return true
}

// NextRound processes the next round of the protocol, active indicates whether
// this participant is active for this round.
func (p *Protocol) NextRound(active bool) (toSend *OutputMessage, output []types.Hash20) {
	p.mu.Lock()
	defer p.mu.Unlock()
	defer func() { p.round++ }()
	// We are starting a new iteration build objects
	if p.round.Round() == 0 {
		p.Ti = append(p.Ti, make(map[types.Hash20][]types.Hash20))
		p.hardLocked = append(p.hardLocked, false)
		p.Li = append(p.Li, nil)
		p.Vi = make([][]types.Hash20, 4)
	}
	if p.round >= 0 && p.round <= 3 {
		p.Vi[p.round] = p.tgg.RetrieveThresholdMessages(-1, 5-uint8(p.round))
	}
	j := p.round.Iteration()
	switch p.round.Type() {
	case Preround:
		if !active {
			return nil, nil
		}
		// Gossip initial values
		return &OutputMessage{
			Round:  p.round,
			Values: p.Si,
		}, nil
	case HardLock:
		if j > 0 {
			values := p.tgg.RetrieveThresholdMessages(NewAbsRound(j-1, 5), 4)
			setHash, _ := matchInPreviousIterations(values, p.Ti, j)
			if setHash != nil {
				p.Li[j] = setHash
				p.hardLocked[j] = true

			}
		}
	case SoftLock:
		if j > 0 {
			// If a commit from the previous iteration reached threshold with
			// at least grade 3 and it's value was added to valid values in any
			// iteration then set the lock to that value.
			values := p.tgg.RetrieveThresholdMessages(NewAbsRound(j-1, 5), 3)
			setHash, _ := matchInPreviousIterations(values, p.Ti, j)
			if setHash != nil {
				p.Li[j] = setHash
			}
		}
		return nil, nil
	case Propose:
		if !active {
			return nil, nil
		}
		var set []types.Hash20
		if j > 0 {
			// Check if values received from thresh gossip, we can only do this
			// safely if we know j > 0, so this can't be done before that
			// check.
			values := p.tgg.RetrieveThresholdMessages(NewAbsRound(j-1, 5), 2)
			_, set = matchInPreviousIterations(values, p.Ti, j)
		}
		// If we didn't get a set from threshold gossip then use our local candidate
		if set == nil {
			set = p.Vi[1]
		}

		// Send proposal to peers
		return &OutputMessage{
			Round:  p.round,
			Values: set,
		}, nil
	case Commit:
		candidates := p.gc.RetrieveGradecastedMessages(NewAbsRound(j, 2))
		for _, c := range candidates {
			if isSubset(c.values, p.Vi[3]) {
				// Add to valid proposals for this iteration
				p.Ti[j][toHash(c.values)] = c.values
			}
		}
		if !active {
			return nil, nil
		}

		var mm *OutputMessage
		if p.hardLocked[j] {
			mm = &OutputMessage{
				Round:  NewAbsRound(j, 5),
				Values: []types.Hash20{*p.Li[j]},
			}
		} else {
			for _, c := range candidates {
				candidateHash := toHash(c.values)
				// Check to see if valid proposal for this iteration
				// Round 5 condition c
				_, ok := p.Ti[j][candidateHash]
				if !ok {
					continue
				}
				// Check leader
				// Round 5 condition d
				if !p.lc.IsLeader(c.id, NewAbsRound(j, 2)) {
					continue
				}
				// Check grade
				// Round 5 condition e
				if c.grade < 2 {
					continue
				}
				// Check subset of threshold values at round 3
				// Round 5 condition f
				if !isSubset(c.values, p.Vi[2]) {
					continue
				}
				// Check superset of highest graded values or we received a
				// commit for the previous iteration from thresh-gossip for
				// this set with grade >= 1.
				// Round 5 condition g
				if !isSubset(p.Vi[0], c.values) {
					// Check for received message
					lastIterationCommit := NewAbsRound(j-1, 5)
					values := p.tgg.RetrieveThresholdMessages(lastIterationCommit, 1)

					// If the candidate is not superset of highest graded
					// values and no commit for that candidate was received in
					// the previous iteration with grade >= 1 then we go to the
					// next candidate.
					if !slices.Contains(values, candidateHash) {
						continue
					}
				}
				// Locked value for this iteration is nil or matches set
				//
				// Round 5 condition h
				if !(p.Li[j] == nil || *p.Li[j] == candidateHash) {
					continue
				}
				mm = &OutputMessage{
					Round:  NewAbsRound(j, 5),
					Values: []types.Hash20{candidateHash},
				}
				break
			}
		}
		return mm, nil
	case Notify:
		// Case 1
		values := p.tgg.RetrieveThresholdMessages(NewAbsRound(j-1, 6), 5)
		resultHash, result := matchInPreviousIterations(values, p.Ti, j)
		if resultHash != nil {
			return &OutputMessage{
				Round:  NewAbsRound(j, 6),
				Values: []types.Hash20{*resultHash},
			}, result
		}
		// Case 2
		if active {
			values := p.tgg.RetrieveThresholdMessages(NewAbsRound(j, 5), 5)
			setHash, _ := matchInIteration(values, p.Ti, j)
			return &OutputMessage{
				Round:  NewAbsRound(j, 6),
				Values: []types.Hash20{*setHash},
			}, nil

		}
	}

	return nil, nil
}

type OutputMessage struct {
	Round  AbsRound
	Values []types.Hash20
}

// with int8 we have a max iterations of 17 before we overflow.
// AbsRound (short for absolute round) represents a round of the hare protocol. The hare protocol progresses in iterations and there are 7 rounds per iteration, except for the first iteration which has a one off pre-round  the abs round starts at -1 and increments for each successive round.
//
// from the abs round both round and message type can be
// the iteration and the round within that iteration can be derived.
type AbsRound int8

// NewAbsRound constructs a new AbsRound from the given iteration (j) and round (r).
func NewAbsRound(j, r int8) AbsRound {
	return AbsRound(j*7 + r)
}

func (r AbsRound) Round() int8 {
	return int8(r) % 7
}

func (r AbsRound) Type() MsgType {
	return MsgType(r % 7)
}

func (r AbsRound) Iteration() int8 {
	return int8(r) / 7
}
