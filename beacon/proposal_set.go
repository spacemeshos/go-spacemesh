package beacon

type proposalSet map[Proposal]struct{}

func (vs proposalSet) list() proposalList {
	votes := make(proposalList, 0)

	for vote := range vs {
		votes = append(votes, vote)
	}

	return votes
}

func (vs proposalSet) sort() proposalList {
	return vs.list().sort()
}
