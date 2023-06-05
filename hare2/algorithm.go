// package hare2 has an improved version of hare that solves the problem of
// excessive bandwidth usage and also allows for partial agreement over the set
// of participants.
//
// The design of this package strives to remove any extraneous information from
// the protocol's implementation so as to simplify the logic, and reduce the
// dependencies of this package. To this end for the most part the protocol
// does not contain or handle any errors (except for Handle), all error
// generating code has been extracted.
//
// It also strives to reduce nesting/deep call stacks, so instead of nesting
// objects inside each other we compose their functionality, this makes testing
// individual components easier because they do less and it removes the need to
// mock in many cases, it also aids at keeping the code error free, since there
// is less chance that we need to handle an error from a nested component and
// finally keeps the code flexible, since if we want to change where the output
// of threshGossip goes we don't need to modify threshGossip or re-implement
// gradedGossip.
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
package hare2

import (
	"github.com/spacemeshos/go-scale"
	"golang.org/x/exp/slices"

	"github.com/spacemeshos/go-spacemesh/codec"
)

// I'm going to remove signature verification from these protocols to keep them
// simple, we assume signature verification is done up front.

// Also I'm trying to ensure that any processing that could result in an error
// is also removed from the protocols.

// not sure exactly what to put here, but the output should be the grade of key

type (
	MsgType            uint8
	GradedGossipResult uint8
	Hash20             [20]byte
)

const (
	Preround MsgType = iota
	Propose
	Commit
	Notify

	SendValue GradedGossipResult = iota
	SendEquivocationProof
	DropMessage

	// d is the degree parameter of both the graded gossip and threshold gossip
	// pprotocols, it is the number of different grades that can be assigned.
	d = 5
)

type Wrapper struct {
	msg, sig []byte
}

func (m *Wrapper) DecodeScale(d *scale.Decoder) (int, error) {
	return 0, nil
}

type Msg struct {
	sid, key []byte
	values   []Hash20
	round    int8
}

func (m *Msg) DecodeScale(d *scale.Decoder) (int, error) {
	return 0, nil
}

type NetworkGossiper interface {
	Gossip(msg []byte) error
}

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
	ReceiveMsg(id Hash20, values []Hash20, msgRound AbsRound, grade uint8) // (sid, r, v, d + 1 − s)

	// Since gradecast always outputs at msgRound+3 it is asumed that callers
	// Only call this function at msgRound+3. Returns all sets of values output
	// by gradcast at msgRound+3 along with their grading.
	RetrieveGradecastedMessages(msgRound AbsRound) []GradecastedSet
}

type LeaderChecker interface {
	IsLeader(vk Hash20, round AbsRound) bool
}

func verify(sig, data []byte) error {
	return nil
}

func gradeKey3(key []byte) uint8 {
	return 0
}

func gradeKey5(key []byte) uint8 {
	return 0
}

type Handler struct {
	ng  NetworkGossiper
	gc  Gradecaster
	gg  GradedGossiper
	tgg TrhesholdGradedGossiper
}

// Can we remove sid from the protocols, probably, say we have a separate instance for each sid.
//
// TODO remove notion of sid from this method so assume we have a separate
// method that does the decoding key verifying ... etc and we jump into this
// method with just the required params at the point we grade keys.
func (h *Handler) HandleMsg(msg []byte) error {
	w := &Wrapper{}

	// These three cases we are dropping the message.
	err := codec.Decode(msg, w)
	if err != nil {
		return err
	}
	err = verify(w.sig, w.msg)
	if err != nil {
		return err
	}
	m := &Msg{}
	err = codec.Decode(msg, m)
	if err != nil {
		return err
	}
	var g uint8
	r := AbsRound(m.round)
	switch r.Type() {
	case Propose:
		g = gradeKey3(m.key)
	default:
		g = gradeKey5(m.key)
	}
	vk := hashBytes(m.key)
	valuesHash := toHash(m.values)
	var values []Hash20
	var result GradedGossipResult
	result = h.gg.ReceiveMsg(vk, valuesHash, r, g)
	switch result {
	case DropMessage:
		// Indicates prior equivocation, drop the message.
		return nil
	case SendEquivocationProof:
		// Indicates new instance of equivocation notify gradcast or threshold
		// gossip with nil values.
		values = nil
	case SendValue:
		// Indicates valid message, set values to message values.
		values = m.values
	}
	// Forward original message to peers
	err = h.ng.Gossip(msg)
	if err != nil {
		return err
	}

	// Pass results to gradecast or threshold gossip.
	// Only proposals are gradecasted, all other message types are passed to
	// threshold gossip.
	if r.Type() == Propose {
		// Send to gradecast
		h.gc.ReceiveMsg(vk, values, r, g)
	} else {
		// Pass result to threshold gossip
		h.tgg.ReceiveMsg(vk, values, r, g)
	}
	return nil
}

type Protocol struct {
	// round encodes both interation and current iteration round in a single value.
	round AbsRound
	// Each index "i" holds an indication of whether iteration "i" is hard locked
	hardLocked []bool
	// Each index "i" holds the locked value for iteration "i"
	Li []*Hash20
	// Each index "i" holds values considered valid up to round "i+1"
	Vi [][]Hash20
	// Each index "i" holds a map of sets of values from valid proposals
	// indexed by their hash received in iteration "i"
	Ti     []map[Hash20][]Hash20
	Si     []Hash20
	tgg    TrhesholdGradedGossiper
	gc     Gradecaster
	lc     LeaderChecker
	active bool
}

func toHash(values []Hash20) Hash20 {
	return Hash20{}
}

func hashBytes(v []byte) Hash20 {
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

func (p *Protocol) NextRound() (toSend *miniMsg, output []Hash20) {
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
	switch p.round {
	case -1:
		if !p.active {
			return nil, nil
		}
		// Gossip initial values
		return &miniMsg{
			round:  p.round,
			values: p.Si,
		}, nil
	case 0:
		if j > 0 {
			values := p.tgg.RetrieveThresholdMessages(NewAbsRound(j-1, 5), 4)
			setHash, _ := matchInPreviousIterations(values, p.Ti, j)
			if setHash != nil {
				p.Li[j] = setHash
				p.hardLocked[j] = true

			}
		}
	case 1:
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
	case 2:
		if !p.active {
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
		return &miniMsg{
			round:  p.round,
			values: set,
		}, nil
	case 5:
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
		if !p.active {
			return nil, nil
		}

		var mm *miniMsg
		if p.hardLocked[j] {
			mm = &miniMsg{
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
				mm = &miniMsg{
					round:  NewAbsRound(j, 5),
					values: []Hash20{candidateHash},
				}
				break
			}
		}
		return mm, nil
	case 6:
		// Case 1
		values := p.tgg.RetrieveThresholdMessages(NewAbsRound(j-1, 6), 5)
		resultHash, result := matchInPreviousIterations(values, p.Ti, j)
		if resultHash != nil {
			return &miniMsg{
				round:  NewAbsRound(j, 6),
				values: []Hash20{*resultHash},
			}, result
		}
		// Case 2
		if p.active {
			values := p.tgg.RetrieveThresholdMessages(NewAbsRound(j, 5), 5)
			setHash, _ := matchInIteration(values, p.Ti, j)
			return &miniMsg{
				round:  NewAbsRound(j, 6),
				values: []Hash20{*setHash},
			}, nil

		}
	}

	return nil, nil
}

type miniMsg struct {
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
