package beacon

type proposalSet map[string]struct{}

func (vs proposalSet) list() proposalList {
	votes := make(proposalList, 0)

	for vote := range vs {
		votes = append(votes, Proposal{
			Value: []byte(vote),
		})
	}

	return votes
}

func (vs proposalSet) sort() proposalList {
	return vs.list().sort()
}
