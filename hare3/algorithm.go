package hare3

import (
	"github.com/spacemeshos/go-scale"
	"github.com/spacemeshos/go-spacemesh/codec"
)

// Hare 2 below --------------------------------------------------

// Don't really want this to be part of the impl because it introduces errors
// that we don't want or really need.
//
// Although actually if we look at how hare handles sending message errors it
// just logs them, so we could do the same and not expose the error, which
// probably makes more sense than trying to propagate up an action (send or
// drop with possibly a message ) from the algorithm to pass to some network
// component
// type NetworkGossiper interface {
// 	NetworkGossip([]byte) error
// }

// type Action uint32

// const (
// 	send Action = iota
// 	drop
// )

// type ByzantineGossip interface {
// 	Gossip([]byte) (action Action, output []byte)
// }

//--------------------------------------------------
// Take 2 below

// type NetworkGossiper interface {
// 	NetworkGossip([]byte)
// }

// type ByzantineGossiper interface {
// 	Gossip([]byte) (output []byte)
// }

// type ThresholdGossiper interface {
// 	Gossip([]byte) (output []byte)
// }

// messages have iteration round

// Hmm can i have three receive interfaces and three send internfaces and then hook them up in differing order
// Seems I need to have 2 flows to account for the threshold and gradecast systems.
// E.G ByzantineReceiver -> ThresholdReceiver -> ProtocolReceiver
//     ByzantineReceiver -> GradecastSender -> ProtocolReceiver
//     ProtocolSender -> ThresholdSender -> ByzantineSender
//     ProtocolSender -> GradecastSender -> ByzantineSender

//--------------------------------------------------

// Take 3 below

// so actually rather than the above approach I am now leaning towards not
// nesting the protocols and simply connecting them up to provide the output of
// one to the next. This keeps things nice and flat and very isolated.

// I'm going to remove signature verification from these protocols to keep them
// simple, we assume signature verification is done up front.

// Hare 3 below --------------------------------------------------

// Hare 3 is pretty similare just with an underlying PKI grading protocol.

// type GradedGossiper interface {
// 	// Gossip takes the given sid (session id) value and verification key (key)
// 	Gossip(sid, value, key, sig []byte) (s, v, k []byte, grade uint8)
// }

// But we want to take signatures out so that we don't deal with errors

type KeyGrader any

// not sure exactly what to put here, but the output should be the grade of key

type Wrapper struct {
	msg, sig []byte
}

func (m *Wrapper) DecodeScale(d *scale.Decoder) (int, error) {
	return 0, nil
}

type (
	MsgType uint8
	Hash20  [20]byte
)

const (
	Preround MsgType = iota
	Propose
	Commit
	Notify

	grades = 5
)

type Msg struct {
	sid, value, key []byte
	round           int8
}

func (m *Msg) DecodeScale(d *scale.Decoder) (int, error) {
	return 0, nil
}

type NetworkGossiper interface {
	// ReceiveMsg takes the given sid (session id) value, key (verification
	// key) and grade and processes them. If a non nil v is returned the
	// originally received input message should be forwarded to all neighbors
	// and (sid, value, key, grade) is considered to have been output.
	Gossip(msg []byte) error
}

// TODO remove some params from here need to combine the value and round
type GradedGossiper interface {
	// ReceiveMsg takes the given sid (session id) value, key (verification
	// key) and grade and processes them. If a non nil v is returned the
	// originally received input message should be forwarded to all neighbors
	// and (sid, value, key, grade) is considered to have been output.
	ReceiveMsg(value []byte, round AbsRound, grade uint8) (v []byte)
}

type TrhesholdGradedGossiper interface {
	// Threshold graded gossip takes outputs from graded gossip, which are in
	// fact sets of values not single values, and returns sets of values that
	// have reached the required threshold, along with the grade for that set.
	ReceiveMsg(values []Hash20, msgRound AbsRound, grade uint8) // (sid, r, v, d + 1 − s)
	// Increment round increments the current round
	IncRound() (v [][]byte, g uint8)

	// Tuples of sid round and value are output once only, since they could
	// only be output again in a later round with a lower grade. This function
	// outputs the values that were part of messages sent at msgRound and
	// reached the threshold at round. It also outputs the grade assigned which
	// will be 5-(round-msgRound).
	RetrieveThresholdMessages(msgRound, round AbsRound) (values []Hash20, grade uint8)
}

type GradecastedSet struct {
	values []Hash20
	grade  uint8
}

type Gradecaster interface {
	ReceiveMsg(values []Hash20, msgRound AbsRound, grade uint8) // (sid, r, v, d + 1 − s)
	// Increment round increments the current round
	IncRound() (v [][]byte, g uint8)

	// Since gradecast always outputs at round r+3 it is asumed that callers
	// Only call this function at msgRound + 3. Returns all sets of values output by
	// gradcast at that msgRound + 3 along with their grading.
	RetrieveGradecastedMessages(msgRound AbsRound) []GradecastedSet
}

// ThresholdState holds the graded votes for a value and also records if this
// value has been retrieved.
type ThresholdState struct {
	votes     [grades]uint16
	retrieved bool
}

func (s *ThresholdState) Vote(grade uint8) {
	// Grade is 1 indexed so we subtract 1
	s.votes[grade-1]++
}

func (s *ThresholdState) CumulativeVote(minGrade uint8) uint16 {
	var count uint16

	// Grade is 1 indexed so we subtract 1
	for i := minGrade - 1; i < grades; i++ {
		count += s.votes[i]
	}
	return count
}

type testThresh struct {
	count  map[AbsRound]map[Hash20]*ThresholdState
	thresh uint16
}

func (t *testThresh) ReceiveMsg(values []Hash20, msgRound AbsRound, grade uint8) {
	for _, v := range values {
		// Get state for received Value
		state, ok := t.count[msgRound][v]
		if !ok {
			state = &ThresholdState{}
			t.count[msgRound][v] = state
		}
		// If the value has already been retrieved then skip any update
		if state.retrieved {
			continue
		}
		state.Vote(grade)
	}
}

// Gets the messages belonging to msgRound that have met the threshold at round.
// TODO need to make sure round is never -1
// TODO do I allow messages to be retreived more than once?
func (t *testThresh) RetrieveThresholdMessages(msgRound, round AbsRound) (values []Hash20, grade uint8) {
	var result []Hash20
	// The min grade allowed to be considered to reach the threshold at this
	// round.
	minGrade := grades + 1 - uint8((round - msgRound))
	for v, state := range t.count[msgRound] {
		if state.CumulativeVote(minGrade) > t.thresh {
			state.retrieved = true
			result = append(result, v)
		}
	}
	return result, minGrade
}

type ValueSet interface {
	// Threshold graded gossip takes outputs from graded gossip, which are in
	// fact sets of values not single values, and returns sets of values that
	// have reached the required threshold, along with the grade for that set.
	Hash() []byte
	// Increment round increments the current round
	Values() [][]byte
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

// with int8 we have a max iterations of 17 before we overflow.
// func absRound(iteration int8, round int8) int8 {
// 	return 7*iteration + round
// }

// Can we remove sid from the protocols, probably, say we have a separate instance for each sid.
//
// TODO remove notion of sid from this method so assume we have a separate
// method that does the decoding key verifying ... etc and we jump into this
// method with just the required params at the point we grade keys.
func HandleMsg(msg []byte) error {
	var ng NetworkGossiper
	var gg GradedGossiper
	var tgg TrhesholdGradedGossiper
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
	v := gg.ReceiveMsg(m.value, r, g)
	if v == nil {
		// Equivocation we drop the message
		return nil
	}
	// Send the message to peers
	ng.Gossip(msg)

	if r.Type() == Propose {
		// store it
		return nil
	}

	// Somehow break down the value into its values
	var values []Hash20
	// Pass result to threshold gossip
	tgg.ReceiveMsg(values, r, g)
	return nil
}

type Protocol struct {
	iteration   int8
	hardLocked  []bool
	lockedValue []Hash20
	values      []Hash20
	// Each index "i" holds values considered valid up to round "i+1"
	Vi [][]Hash20
	// Each index "i" holds a map of sets of values from valid proposals
	// indexed by their hash received in iteration "i"
	Ti     []map[Hash20][]Hash20
	Si     []Hash20
	Shash  Hash20
	S      []Hash20
	round  AbsRound
	tgg    TrhesholdGradedGossiper
	gc     Gradecaster
	active bool
}

func toHash(values []Hash20) Hash20 {
	return Hash20{}
}

// Given a slice of candidate set hashes and a slice of sets of valid set hashes
// returns the first candidate that appears in the valid hashes, along with it's set.
func findMatch(candidates []Hash20, validSets []map[Hash20][]Hash20, j int8) (*Hash20, []Hash20) {
	for i := 0; i <= int(j); i++ {
		for _, v := range candidates {
			set, ok := validSets[i][v]
			if ok {
				return &v, set
			}
		}
	}
	return nil, nil
}

func wrapRetreiveThresh(tgg TrhesholdGradedGossiper, round AbsRound) []Hash20 {
	values, _ := tgg.RetrieveThresholdMessages(NewAbsRound(round.Iteration()-1, 5), round)
	return values
}

func isSubset(subset, superset []Hash20) bool {
	return true
}

func (p *Protocol) NextRound() *miniMsg {
	if p.round >= 0 && p.round <= 3 {
		p.Vi[p.round], _ = p.tgg.RetrieveThresholdMessages(-1, p.round)
	}
	// We are starting a new iteration build objects
	if p.round.Round() == 0 {
		p.Ti = append(p.Ti, make(map[Hash20][]Hash20))
		p.Vi = append(p.Vi, nil)
		p.hardLocked = append(p.hardLocked, false)
		p.lockedValue = append(p.lockedValue, nil)
	}
	j := p.round.Iteration()
	switch p.round {
	case -1:
		if !p.active {
			return nil
		}
		// Gossip initial values
		return &miniMsg{
			round:  p.round,
			values: p.Si,
		}
	case 0:
	case 1:
	case 2:
		if !p.active {
			return nil
		}
		// in the paper it says up to the beginning of round 2 if a message was
		// received from threshold gossip with grade 2 or greater. But since
		// I'm not delivering messages real time from thresh gossip to the
		// protocol and I just look when i need the message, I look at the last
		// possible point, so the returned grade will be 2 and therefore I
		// don't need the grade. Will this cause problems for me do I need to
		// allow for the same message to be retreived more than once?
		//
		// Hmm case 2 of round 6 asks if the protocol has received (commit R(j,
		// 5), S, 5) from thresh gossip, could we just look again? Providing
		// the right round as context. I think it works?
		var setHash *Hash20
		var set []Hash20
		if j > 0 {
			// Check if values recieved from thresh gossip, we can only do this
			// safely if we know j > 0, so this can't be done before that
			// check.
			values, _ := p.tgg.RetrieveThresholdMessages(NewAbsRound(j-1, 5), p.round)
			setHash, set = findMatch(values, p.Ti, j)
		}
		// If we didn't get a set from threshold gossip then use our local candidate
		if setHash == nil {
			h := toHash(p.Vi[4])
			setHash = &h
			set = p.Vi[4]
		}

		// Update instance variables
		p.Shash = *setHash
		p.S = set
		// Return message to be sent to peers
		return &miniMsg{
			round:  p.round,
			values: p.S,
		}
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
			return nil
		}

		var mm *miniMsg
		if p.hardLocked[j] {
			mm = &miniMsg{
				round:  NewAbsRound(j, 5),
				values: []Hash20{p.lockedValue[j]},
			}
		} else {
		}
		return mm
	case 6:
	}

	p.round++
	return nil
}

// Preround returns a set of values to send in the preround.
func (p *Protocol) DoPreround() *miniMsg {
	return &miniMsg{-1, p.values}
}

func (p *Protocol) DoHardLock() *miniMsg {
	return nil
}

func (p *Protocol) DoSoftlock() *miniMsg {
	return nil
}

// propose and commit both return iteration and value that can be used to send a message of the appropriate type.
func (p *Protocol) DoPropose() *miniMsg {
	return nil
}

func (p *Protocol) DoCommit() *miniMsg {
	return nil
}

func (p *Protocol) DoNotify() *miniMsg {
	return nil
}

type miniMsg struct {
	round  AbsRound
	values []Hash20
}

type AbsRound int8

func NewAbsRound(j, r int8) AbsRound {
	return AbsRound(j*7 + r)
}

func (r AbsRound) Round() int8 {
	return r % 7
}

func (r AbsRound) Type() MsgType {
	return MsgType(r % 7)
}

func (r AbsRound) Iteration() int8 {
	return int8(r) / 7
}

// To run this we just want a loop that pulls from 2 channels a timer channel and a channel of messages
