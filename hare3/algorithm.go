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

// GradedGossiper works as a filter, for the given message inputs it returns
// one of 3 results indicating what action to take with the message it was
// invoked for.
type GradedGossiper interface {
	// ReceiveMsg accepts a hash identifying the message an id identifying the
	// originator of the message, a hash identifying the value in the message
	// the message round and a grade. It returns a value indicating what action
	// to take with the message and the hash of an equivocating message if
	// equivocation was detected.
	//
	// There are 3 different results that graded gossip can output:
	//
	// * SendValue indicates that the message be forwarded to downstream sub
	// protocols (Gradecaster & TrhesholdGradedGossiper) and ultimately that
	// the message should be relayed to the network. Note that the first
	// message in a round from a known malicious identity needs to be
	// forwarded, since hare3 includes the number of known malicious entities
	// in its threshold calculations. Also it is necessary to forward it so
	// that the rest of the network can also include the malicious identity in
	// their threshold calculations. Messages from known malicious entities are
	// signified to GradedGossiper by having a nil valueHash.
	//
	// * SendEquivocationProof is returned in the case that a new equivocation
	// has been detected, the equivocationHash will be the hash of the prior
	// message from the same identity. In this case again the equivocating
	// message still needs to be forwarded, for the same reasons as above.
	//
	// * DropMessage is returned in the case of any further messages from
	// entities for which we have already forwarded a malicious message this
	// round. In this case the message should be ignored and not relayed to the
	// network.
	//
	// Note that there can be at most 2 messages forwarded per identity per
	// round, either:
	//
	// * One good message.
	// * One malicious message from a known eqiuvotator.
	// * One good message followed by a malicious message indicating equivocation.
	ReceiveMsg(hash, id types.Hash20, valueHash *types.Hash20, round AbsRound, grade uint8) (result GradedGossipResult, equivocationHash *types.Hash20)
}

type DefaultGradedGossiper struct{}

// ReceiveMsg implements GradedGossiper.
func (*DefaultGradedGossiper) ReceiveMsg(
	hash types.Hash20,
	id types.Hash20,
	valueHash *types.Hash20,
	round AbsRound,
	grade uint8,
) (result GradedGossipResult, equivocationHash *types.Hash20) {
	return SendValue, nil
}

// ThresholdState holds the graded votes for a value and also records if this
// value has been retrieved.
type GradedVotes struct {
	votes [d]uint16
}

func (s *GradedVotes) Vote(grade uint8) {
	// Grade is 1 indexed so we subtract 1
	s.votes[grade-1]++
}

func (s *GradedVotes) CumulativeVote(minGrade uint8) uint16 {
	var count uint16

	// Grade is 1 indexed so we subtract 1
	for i := minGrade - 1; i < d; i++ {
		count += s.votes[i]
	}
	return count
}

// TrhesholdGradedGossiper acts as a specialized value store, it ingests
// messages and provides a mechanism to retrieve values that appeared in some
// threshold of ingested messages with a given grade. Messages received more
// than d (the degree of the trheshold gossip instance) rounds late are
// ignored.
type TrhesholdGradedGossiper interface {
	// ReceiveMsg ingests the given message inputs, for later retrieval through
	// RetrieveThresholdMessages. The inputs are the message sender, the values
	// contained in the message, the message round, the current round and a
	// grade, the grade must be between 1 and d inclusive. If values is nil it
	// indicates a message from a malicious entity.
	//
	// Note that since this component is downstream from graded gossip it
	// expects at most 2 messages per identity per round.
	ReceiveMsg(senderID types.Hash20, values []types.Hash20, msgRound, currRound AbsRound, grade uint8)

	// This function outputs the values that were part of messages sent at
	// msgRound and reached the threshold with grade at least minGrade by round
	// "msgRound + d + 1 - minGrade". minGrade must be between 1 and d inclusive.
	RetrieveThresholdMessages(msgRound AbsRound, minGrade uint8) (values []types.Hash20)
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

type votes struct {
	grade  uint8
	values []types.Hash20
}

type DefaultThresholdGradedGossiper struct {
	// count maps the message round to sender ids to values to the grade of the vote for that value.
	//
	// E.G. The votes received for a value sent in a message with round R with
	// at least grade 3 would be:
	// count[R][V].CumulativeVote(3)
	count map[AbsRound]map[types.Hash20]votes

	// maliciousVotes tracks the number of detected malicious parties by grade.
	// We don't need to track the sender ID of malicious parties since
	// GradedGossiper limits us to one malicious message per id per round.
	maliciousVotes map[AbsRound]*GradedVotes

	// Values with more votes than the threshold are considered to have passed
	// the threshold.
	threshold uint16
}

func NewDefaultThresholdGradedGossiper(threshold uint16) *DefaultThresholdGradedGossiper {
	return &DefaultThresholdGradedGossiper{
		count:          make(map[AbsRound]map[types.Hash20]votes),
		maliciousVotes: make(map[AbsRound]*GradedVotes),
		threshold:      threshold,
	}
}

func NewGradedVotes() *GradedVotes {
	return &GradedVotes{}
}

// ReceiveMsg implements TrhesholdGradedGossiper.
func (t *DefaultThresholdGradedGossiper) ReceiveMsg(senderID types.Hash20, values []types.Hash20, msgRound, currRound AbsRound, grade uint8) {
	if currRound-msgRound > d {
		// Message received too late.
		return
	}
	if values != nil {
		// Ensure there is a map entry for this round.
		votesBySender := Ensure(t.count, msgRound)
		// Store the votes and grade against the sender id.
		votesBySender[senderID] = votes{
			values: values,
			grade:  grade,
		}
	} else {
		// Delete any good votes that the id may have had
		delete(t.count[msgRound], senderID)
		// Record the grade for this malicious vote, its not important to
		// record the sender id for this malicious vote since we should receive
		// at most one malicious message per identity per round from graded gossip.
		votesByRound := EnsureValue(t.maliciousVotes, msgRound, NewGradedVotes)
		votesByRound.Vote(grade)
	}
}

// RetrieveThresholdMessages implements TrhesholdGradedGossiper.
func (t *DefaultThresholdGradedGossiper) RetrieveThresholdMessages(msgRound AbsRound, minGrade uint8) (values []types.Hash20) {
	// maps values to the good votes they have received.
	goodVotes := make(map[types.Hash20]*GradedVotes)

	// Tally all the votes from all senders for each value
	votesBySender := Ensure(t.count, msgRound)
	for _, v := range votesBySender {
		for _, value := range v.values {
			gv := EnsureValue(goodVotes, value, NewGradedVotes)
			gv.Vote(v.grade)
		}
	}
	var results []types.Hash20
	// Find out the number of maliciousVotes for this round and grade.
	votesByRound := EnsureValue(t.maliciousVotes, msgRound, NewGradedVotes)
	maliciousVote := votesByRound.CumulativeVote(minGrade)

	// Find all values where the good and malicious votes sum to greater than
	// the threshold and there is at least one good vote.
	for value, v := range goodVotes {
		goodVote := v.CumulativeVote(minGrade)
		if goodVote > 0 && goodVote+maliciousVote > t.threshold {
			results = append(results, value)
		}
	}
	return results
}

type GradecastedSet struct {
	vk     types.Hash20
	values []types.Hash20
	grade  uint8
}

// Gradecaster acts as a specialized value store, it ingests messages that can
// be retrieved after 3 rounds. Retrieved messages will be graded either 1 or 2
// based on the input grade and when they were ingested relative to the
// msgRound. Messages received too late or with insufficient grades will not be
// retrievable. Subsequent malicious messages can also render previously good
// messages un-retrievable.
type Gradecaster interface {
	// ReceiveMsg ingests the given message inputs, for later retrieval through
	// RetrieveGradecastedMessages. The inputs are the id of the message
	// originator, the values contained in the message, the message round and a
	// grade. If values is nil it indicates a message from a malicious entity.
	// Note that since this component is downstream from graded gossip it
	// expects at most 2 messages per identity per round.
	ReceiveMsg(id types.Hash20, values []types.Hash20, msgRound, currRound AbsRound, grade uint8)

	// Since gradecast always outputs at msgRound+3 it is assumed that callers
	// Only call this function at msgRound+3. Returns all sets of values output
	// by gradcast at msgRound+3 along with their grading.
	RetrieveGradecastedMessages(msgRound AbsRound) []GradecastedSet
}

// Tracks the state of a message in gradecast, a nil values indicates that
// there will be no output from gradecast for this message.
type msgState struct {
	values   []types.Hash20
	grade    uint8
	received AbsRound
}

// Returns the gradecast grade between 0-2 inclusive.
func (s msgState) gradecastGrade(msgRound AbsRound) uint8 {
	switch {
	case s.values != nil && s.grade == 3 && s.received-msgRound <= 1:
		// Grade 2 as defined by gradecast is acieved by having a grade of 3
		// from 3 graded gossip and having been received not more than 1 round
		// after the message round.
		return 2
	case s.values != nil && s.grade >= 2 && s.received-msgRound <= 2:
		// Grade 1 as defined by gradecast is acieved by having a grade of 2 or
		// 3 from 3 graded gossip and having been received not more than 2
		// rounds after the message round.
		return 1
	default:
		return 0
	}
}

type DefaultGradecaster struct {
	// Maps message round to sender to message state
	messages map[AbsRound]map[types.Hash20]*msgState
}

// ReceiveMsg implements Gradecaster.
func (g *DefaultGradecaster) ReceiveMsg(id types.Hash20, values []types.Hash20, msgRound, currRound AbsRound, grade uint8) {
	senderStates := Ensure(g.messages, msgRound)
	state, ok := senderStates[id]
	if !ok {
		// If there is no entry then add the entry and return
		senderStates[id] = &msgState{
			values:   values,
			grade:    grade,
			received: currRound,
		}
		return
	}
	// If the first thing to be recorded was a malicious message then there's
	// no output from gradecast.
	if state.values == nil {
		return
	}
	// Note that due to being downstream from gradecast the only instance where
	// we can receive a second message from gradecast is that of an
	// equivocating message.
	switch {
	// If the existing entry had a gradecast grade of 2 and this malicious
	// message was received before msgRound+3 then nullify this state.
	case state.gradecastGrade(msgRound) == 2 && currRound-msgRound <= 3:
		state.values = nil

	// If the existing entry had a gradcast grade of 1 and this malicious
	// message was also received up to round 2 then nullify this state.
	case state.gradecastGrade(msgRound) == 1 && currRound-msgRound <= 2:
		state.values = nil
	}
}

// RetrieveGradecastedMessages implements Gradecaster.
func (g *DefaultGradecaster) RetrieveGradecastedMessages(msgRound AbsRound) []GradecastedSet {
	var results []GradecastedSet
	senderStates := Ensure(g.messages, msgRound)
	for sender, state := range senderStates {
		grade := state.gradecastGrade(msgRound)
		if grade > 0 {
			results = append(results, GradecastedSet{
				vk:     sender,
				values: state.values,
				grade:  grade,
			})
		}
	}
	return results
}

type LeaderChecker interface {
	IsLeader(vk types.Hash20, round AbsRound) bool
}

// gradeKey3 returns a grade from 0-3.
func gradeKey3(key []byte) uint8 {
	return 3
}

// gradeKey5 returns a grade from 0-5.
func gradeKey5(key []byte) uint8 {
	return 5
}

// RoundProvider provides a simple way to discover what is the current round.
type RoundProvider struct {
	layerTime     time.Time
	roundDuration time.Duration
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
	tgg TrhesholdGradedGossiper
	gc  Gradecaster
	rp  *RoundProvider
	mu  *sync.Mutex
}

func NewHandler(gg GradedGossiper, tgg TrhesholdGradedGossiper, gc Gradecaster, rp *RoundProvider, protocolMu *sync.Mutex) *Handler {
	return &Handler{
		gg:  gg,
		tgg: tgg,
		gc:  gc,
		rp:  rp,
		mu:  protocolMu,
	}
}

// HandleMsg handles an incoming message, it returns a boolean indicating
// whether the message should be regossipped to peers.
func (h *Handler) HandleMsg(hash types.Hash20, vk []byte, round int8, values []types.Hash20) (bool, *types.Hash20) {
	h.mu.Lock()
	defer h.mu.Unlock()
	r := AbsRound(round)
	var g uint8
	switch r.Type() {
	case Propose:
		g = gradeKey3(vk)
	default:
		g = gradeKey5(vk)
	}
	id := hashBytes(vk)
	var valuesHash *types.Hash20

	// if Values is equal to nil, it is an indication of equivocation, so we leave valuesHash nil to indicate equivocation to graded gossip
	if values != nil {
		h := toHash(values)
		valuesHash = &h
	}

	var gradedGossipValues []types.Hash20
	var equivocationHash *types.Hash20
	// We now have a new rule as explained by Tal, that in fact malfeasance
	// proofs are essentially roundless, i.e. they affect all rounds, so we
	// should just store a map of identity to malfeasance proof inside gg and
	// then check it on exit whenever a message goes through.
	//
	// Nil values signifies a message from a known malicious actor, however we
	// still need to forward the first such message per identity to the network
	// though so that they also insert it into their protocols. So we need an
	// outcome that is send equivocation proof but with a nil equivocation hash
	// in order to handle this case. Internally the implementation will track
	// to see if there was a previous outcome with nil values and if so will
	// drop the message. This then covers the case where we get a second
	// message from a prior known equivocator or we get a third message from an
	// identity equivocating in this round.
	result, eHash := h.gg.ReceiveMsg(hash, id, valuesHash, r, g)
	switch result {
	case DropMessage:
		// Indicates prior equivocation, drop the message.
		return false, nil
	case SendEquivocationProof:
		// Indicates new instance of equivocation notify gradecast or threshold
		// gossip with nil values.
		gradedGossipValues = nil
		// Set the equivocationHash so that the caller can look up the message.
		equivocationHash = eHash
	case SendValue:
		// Indicates valid message, set values to message values.
		gradedGossipValues = values
	}

	curr := h.rp.CurrentRound()
	// Pass results to gradecast or threshold gossip.
	// Only proposals are gradecasted, all other message types are passed to
	// threshold gossip.
	if r.Type() == Propose {
		// Send to gradecast
		h.gc.ReceiveMsg(id, gradedGossipValues, r, curr, g)
	} else {
		// Pass result to threshold gossip
		h.tgg.ReceiveMsg(id, gradedGossipValues, r, curr, g)
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
	Vi [][]types.Hash20
	// Each index "i" holds a map of sets of values from valid proposals
	// indexed by their hash received in iteration "i"
	Ti  []map[types.Hash20][]types.Hash20
	Si  []types.Hash20
	tgg TrhesholdGradedGossiper
	gc  Gradecaster
	lc  LeaderChecker
	mu  *sync.Mutex
}

func NewProtocol(tgg TrhesholdGradedGossiper, gc Gradecaster, lc LeaderChecker, protocolMu *sync.Mutex, si []types.Hash20) *Protocol {
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
	return types.Hash20{}
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
	if p.round >= 0 && p.round <= 3 {
		p.Vi[p.round] = p.tgg.RetrieveThresholdMessages(-1, 5-uint8(p.round))
	}
	// We are starting a new iteration build objects
	if p.round.Round() == 0 {
		p.Ti = append(p.Ti, make(map[types.Hash20][]types.Hash20))
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
				values: []types.Hash20{*p.Li[j]},
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
					values: []types.Hash20{candidateHash},
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
				values: []types.Hash20{*resultHash},
			}, result
		}
		// Case 2
		if active {
			values := p.tgg.RetrieveThresholdMessages(NewAbsRound(j, 5), 5)
			setHash, _ := matchInIteration(values, p.Ti, j)
			return &OutputMessage{
				round:  NewAbsRound(j, 6),
				values: []types.Hash20{*setHash},
			}, nil

		}
	}

	return nil, nil
}

func hashBytes(v []byte) types.Hash20 {
	return types.Hash20{}
}

type OutputMessage struct {
	round  AbsRound
	values []types.Hash20
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
