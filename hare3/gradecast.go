package hare3

import "github.com/spacemeshos/go-spacemesh/common/types"

// GradecastedSet encapsulates the output from gradecast for a given identity.
type GradecastedSet struct {
	id     types.NodeID
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
	ReceiveMsg(id types.NodeID, values []types.Hash20, msgRound, currRound AbsRound, grade uint8)

	// Since gradecast always outputs at msgRound+3 it is assumed that callers
	// Only call this function at msgRound+3. Returns all sets of values output
	// by gradcast at msgRound+3 with grades 1 or 2 along with their grading.
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
	messages map[AbsRound]map[types.NodeID]*msgState
}

func NewDefaultGradecaster() *DefaultGradecaster {
	return &DefaultGradecaster{
		messages: make(map[AbsRound]map[types.NodeID]*msgState),
	}
}

// ReceiveMsg implements Gradecaster.
func (g *DefaultGradecaster) ReceiveMsg(id types.NodeID, values []types.Hash20,
	msgRound, currRound AbsRound, grade uint8,
) {
	if msgRound-currRound > 3 {
		// Messages that arrive more than 3 rounds late have no bearing on the
		// output of gradecast.
		return
	}
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
				id:     sender,
				values: state.values,
				grade:  grade,
			})
		}
	}
	return results
}
