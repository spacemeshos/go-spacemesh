package hare3

import "github.com/spacemeshos/go-spacemesh/common/types"

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

// ThresholdGradedGossiper acts as a specialized value store, it ingests
// messages and provides a mechanism to retrieve values that appeared in some
// threshold of ingested messages with a given grade. Messages received more
// than d (the degree of the threshold gossip instance) rounds late are
// ignored.
type ThresholdGradedGossiper interface {
	// ReceiveMsg ingests the given message inputs, for later retrieval through
	// RetrieveThresholdMessages. The inputs are the message sender, the values
	// contained in the message, the message round, the current round and a
	// grade, the grade must be between 1 and d inclusive. If values is nil it
	// indicates a message from a malicious entity.
	//
	// Note that since this component is downstream from graded gossip it
	// expects at most 2 messages per identity per round.
	ReceiveMsg(senderID types.NodeID, values []types.Hash20, msgRound, currRound AbsRound, grade uint8)

	// This function outputs the values that were part of messages sent at
	// msgRound and reached the threshold with grade at least minGrade by round
	// "msgRound + d + 1 - minGrade". minGrade must be between 1 and d inclusive.
	RetrieveThresholdMessages(msgRound AbsRound, minGrade uint8) (values []types.Hash20)
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
	count map[AbsRound]map[types.NodeID]votes

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
		count:          make(map[AbsRound]map[types.NodeID]votes),
		maliciousVotes: make(map[AbsRound]*GradedVotes),
		threshold:      threshold,
	}
}

func NewGradedVotes() *GradedVotes {
	return &GradedVotes{}
}

// ReceiveMsg implements ThresholdGradedGossiper.
func (t *DefaultThresholdGradedGossiper) ReceiveMsg(senderID types.NodeID, values []types.Hash20, msgRound, currRound AbsRound, grade uint8) {
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

// RetrieveThresholdMessages implements ThresholdGradedGossiper.
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
