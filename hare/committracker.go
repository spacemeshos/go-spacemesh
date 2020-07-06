package hare

type commitTrackerProvider interface {
	OnCommit(msg *Msg)
	HasEnoughCommits() bool
	BuildCertificate() *certificate
	CommitCount() int
}

// commitTracker tracks commit messages and build the certificate according to the tracked messages.
type commitTracker struct {
	seenSenders map[string]bool // tracks seen senders
	commits     []*Message      // tracks Set->Commits
	proposedSet *Set            // follows the set who has max number of commits
	committee   Committee       // nodes Committee
	total       uint64
}

func newCommitTracker(proposedSet *Set, committee Committee) *commitTracker {
	ct := &commitTracker{}
	ct.seenSenders = make(map[string]bool, committee.Size)
	ct.commits = make([]*Message, 0, committee.Size)
	ct.proposedSet = proposedSet
	ct.committee = committee
	return ct
}

// OnCommit tracks the given commit message
func (ct *commitTracker) OnCommit(msg *Msg) {
	if ct.proposedSet == nil { // no valid proposed set
		return
	}

	if ct.HasEnoughCommits() {
		return
	}

	pub := msg.PubKey.String()
	if ct.seenSenders[pub] {
		return
	}

	ct.seenSenders[pub] = true

	s := NewSet(msg.InnerMsg.Values)
	if !ct.proposedSet.Equals(s) { // ignore commit on different set
		return
	}

	// add msg
	ct.commits = append(ct.commits, msg.Message)
	ct.total += ct.committee.WeightOf(pub)
}

// HasEnoughCommits returns true if the tracker can build a certificate, false otherwise.
func (ct *commitTracker) HasEnoughCommits() bool {
	if ct.proposedSet == nil {
		return false
	}

	return ct.total >= ct.committee.Threshold()
}

func (ct *commitTracker) CommitCount() int {
	return len(ct.commits)
}

// BuildCertificate returns a certificate if there are enough commits, nil otherwise
// Returns the certificate if has enough commit Messages, nil otherwise
func (ct *commitTracker) BuildCertificate() *certificate {
	if !ct.HasEnoughCommits() {
		return nil
	}

	c := &certificate{}
	c.Values = ct.proposedSet.ToSlice()
	c.AggMsgs = &aggregatedMessages{}

	// ct.commits contains no more then required by threshold
	c.AggMsgs.Messages = ct.commits

	// optimize msg size by setting Values to nil
	for _, commit := range c.AggMsgs.Messages {
		commit.InnerMsg.Values = nil
	}

	// TODO: set c.AggMsgs.AggSig

	return c
}
