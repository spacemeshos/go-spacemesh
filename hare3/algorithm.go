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
// Additionally there is no use of concurrency in this package.
package hare3

import (
	"golang.org/x/exp/slices"
)

type (
	MsgType            int
	GradedGossipResult uint8
	Hash20             [20]byte
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
	// pprotocols, it is highest grade that can be assigned, grades start at 0.
	d = 5
)

// GradedGossiper works as a filter, for the given messsge inputs it returns
// one of 3 results indicating what action to take with the message it was
// invoked for.
type GradedGossiper interface {
	// ReceiveMsg accepts an id idenifying the originator of the message, a
	// hash identifying the value in the message the message round and a grade.
	// It returns a value indicating what action to take with the message.
	ReceiveMsg(id Hash20, valueHash Hash20, round AbsRound, grade uint8) GradedGossipResult
}

// TrhesholdGradedGossiper acts as a specialized value store, it ingests
// messages and provides a mechanism to retrieve values that appeared in some
// threshold of ingested messages with a given grade.
type TrhesholdGradedGossiper interface {
	// ReceiveMsg ingests the given message inputs, for later retrieval through
	// RetrieveThresholdMessages. The inputs are the id of the message
	// originator, the values contained in the message, the message round and a
	// grade.
	ReceiveMsg(id Hash20, values []Hash20, msgRound AbsRound, grade uint8)

	// This function outputs the values that were part of messages sent at
	// msgRound and reached the threshold with grade at least minGrade by round
	// "msgRound + d + 1 - minGrade".
	RetrieveThresholdMessages(msgRound AbsRound, minGrade uint8) (values []Hash20)
}

type GradecastedSet struct {
	vk     Hash20
	values []Hash20
	grade  uint8
}

// Gradecaster acts as a specialized value store, it ingests messages that can
// be retrieved after 3 rounds. Retrieved messages will be graded either 1 or 2
// based on the input grade and when they were ingested relative to the
// msgRound. Messages received too late or with insufficient grades will not be
// retrievable.
type Gradecaster interface {
	// ReceiveMsg ingests the given message inputs, for later retrieval through
	// RetrieveGradecastedMessages. The inputs are the id of the message
	// originator, the values contained in the message, the message round and a
	// grade.
	ReceiveMsg(id Hash20, values []Hash20, msgRound AbsRound, grade uint8)

	// Since gradecast always outputs at msgRound+3 it is asumed that callers
	// Only call this function at msgRound+3. Returns all sets of values output
	// by gradcast at msgRound+3 along with their grading.
	RetrieveGradecastedMessages(msgRound AbsRound) []GradecastedSet
}

type LeaderChecker interface {
	IsLeader(vk Hash20, round AbsRound) bool
}

// gradeKey3 returns a grade from 0-3.
func gradeKey3(key []byte) uint8 {
	return 0
}

// gradeKey5 returns a grade from 0-5.
func gradeKey5(key []byte) uint8 {
	return 0
}

// Handler performs message handling, there is a handler per instance of each
// hare protocol, and as such the handler does not need to be aware of the
// session id. It is also assumed that some higher level handler performs the
// actions of message decoding and signature verification and key grading,
// leaving this handler to handle the decoded message inputs.
type Handler struct {
	gg  GradedGossiper
	tgg TrhesholdGradedGossiper
	gc  Gradecaster
}

func NewHandler(gg GradedGossiper, tgg TrhesholdGradedGossiper, gc Gradecaster) *Handler {
	return &Handler{
		gg:  gg,
		tgg: tgg,
		gc:  gc,
	}
}

// HandleMsg handles an incoming message, it returns a boolean indicating
// whether the message should be regossiped to peers.
func (h *Handler) HandleMsg(vk []byte, values []Hash20, round int8) bool {
	r := AbsRound(round)
	var g uint8
	switch r.Type() {
	case Propose:
		g = gradeKey3(vk)
	default:
		g = gradeKey5(vk)
	}
	id := hashBytes(vk)
	valuesHash := toHash(values)

	var gradedGossipValues []Hash20
	// Nil values signifies a message from a known malicious actor, in this
	// case we skip passing the message to graded gossip, since we effectively
	// have the output, which is equivocation, we still need to forward the
	// original message to the network though so that they also insert it into
	// their protocols.
	if values != nil {
		result := h.gg.ReceiveMsg(id, valuesHash, r, g)
		switch result {
		case DropMessage:
			// Indicates prior equivocation, drop the message.
			return false
		case SendEquivocationProof:
			// Indicates new instance of equivocation notify gradecast or threshold
			// gossip with nil values.
			gradedGossipValues = nil
		case SendValue:
			// Indicates valid message, set values to message values.
			gradedGossipValues = values
		}
	}
	// Pass results to gradecast or threshold gossip.
	// Only proposals are gradecasted, all other message types are passed to
	// threshold gossip.
	if r.Type() == Propose {
		// Send to gradecast
		h.gc.ReceiveMsg(id, gradedGossipValues, r, g)
	} else {
		// Pass result to threshold gossip
		h.tgg.ReceiveMsg(id, gradedGossipValues, r, g)
	}
	return true
}

type Protocol struct {
	// round encodes both iteration and round in a single value.
	round AbsRound
	// Each index "i" holds an indication of whether iteration "i" is hard locked
	hardLocked []bool
	// Each index "i" holds the locked value for iteration "i"
	Li []*Hash20
	// Each index "i" holds values considered valid up to round "i+1"
	Vi [][]Hash20
	// Each index "i" holds a map of sets of values from valid proposals
	// indexed by their hash received in iteration "i"
	Ti  []map[Hash20][]Hash20
	Si  []Hash20
	tgg TrhesholdGradedGossiper
	gc  Gradecaster
	lc  LeaderChecker
}

func NewProtocol(tgg TrhesholdGradedGossiper, gc Gradecaster, lc LeaderChecker) *Protocol {
	return &Protocol{
		round: AbsRound(-1),
		tgg:   tgg,
		gc:    gc,
		lc:    lc,
	}
}

func (p *Protocol) Round() AbsRound {
	return p.round
}

func toHash(values []Hash20) Hash20 {
	return Hash20{}
}

// Given a slice of candidate set hashes and a slice of sets of valid set
// hashes, one set per iteration, returns the first candidate that appears in
// the valid hashes along with it's set of any iteration before j.
func matchInPreviousIterations(candidates []Hash20, validSets []map[Hash20][]Hash20, j int8) (*Hash20, []Hash20) {
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
func matchInIteration(candidates []Hash20, validSets []map[Hash20][]Hash20, j int8) (*Hash20, []Hash20) {
	validJ := validSets[j]
	for _, v := range candidates {
		set, ok := validJ[v]
		if ok {
			return &v, set
		}
	}
	return nil, nil
}

func isSubset(subset, superset []Hash20) bool {
	return true
}

// NextRound processes the next round of the protocol, active indicates whether
// this participant is active for this round.
func (p *Protocol) NextRound(active bool) (toSend *OutputMessage, output []Hash20) {
	defer func() { p.round++ }()
	if p.round >= 0 && p.round <= 3 {
		p.Vi[p.round] = p.tgg.RetrieveThresholdMessages(-1, 5-uint8(p.round))
	}
	// We are starting a new iteration build objects
	if p.round.Round() == 0 {
		p.Ti = append(p.Ti, make(map[Hash20][]Hash20))
		p.Vi = append(p.Vi, nil)
		p.hardLocked = append(p.hardLocked, false)
		p.Li = append(p.Li, nil)
	}
	j := p.round.Iteration()
	switch p.round.Type() {
	case Preround:
		if !active {
			return nil, nil
		}
		// Gossip initial values
		return &OutputMessage{
			round:  p.round,
			values: p.Si,
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
		var set []Hash20
		if j > 0 {
			// Check if values received from thresh gossip, we can only do this
			// safely if we know j > 0, so this can't be done before that
			// check.
			values := p.tgg.RetrieveThresholdMessages(NewAbsRound(j-1, 5), 2)
			_, set = matchInPreviousIterations(values, p.Ti, j)
		}
		// If we didn't get a set from threshold gossip then use our local candidate
		if set == nil {
			set = p.Vi[4]
		}

		// Send proposal to peers
		return &OutputMessage{
			round:  p.round,
			values: set,
		}, nil
	case Commit:
		candidates := p.gc.RetrieveGradecastedMessages(NewAbsRound(j, 2))
		for _, c := range candidates {
			if c.grade < 1 {
				continue
			}
			if isSubset(c.values, p.Vi[2]) {
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
				round:  NewAbsRound(j, 5),
				values: []Hash20{*p.Li[j]},
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
				if !p.lc.IsLeader(c.vk, NewAbsRound(j, 2)) {
					continue
				}
				// Check grade
				// Round 5 condition e
				if c.grade < 2 {
					continue
				}
				// Check subset of threshold values at round 3
				// Round 5 condition f
				if !isSubset(c.values, p.Vi[3]) {
					continue
				}
				// Check superset of highest graded values or we received a
				// commit for the previous iteration from thresh-gossip for
				// this set with grade >= 1.
				// Round 5 condition g
				if !isSubset(p.Vi[5], c.values) {
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
					round:  NewAbsRound(j, 5),
					values: []Hash20{candidateHash},
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
				round:  NewAbsRound(j, 6),
				values: []Hash20{*resultHash},
			}, result
		}
		// Case 2
		if active {
			values := p.tgg.RetrieveThresholdMessages(NewAbsRound(j, 5), 5)
			setHash, _ := matchInIteration(values, p.Ti, j)
			return &OutputMessage{
				round:  NewAbsRound(j, 6),
				values: []Hash20{*setHash},
			}, nil

		}
	}

	return nil, nil
}

func hashBytes(v []byte) Hash20 {
	return Hash20{}
}

type OutputMessage struct {
	round  AbsRound
	values []Hash20
}

// with int8 we have a max iterations of 17 before we overflow.
type AbsRound int8

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

// To run this we just want a loop that pulls from 2 channels a timer channel and a channel of messages
