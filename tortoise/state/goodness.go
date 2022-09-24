package state

type Condition uint8

func (g Condition) notConsistent() bool {
	return g == conditionNotConsistent
}

func (g Condition) abstained() bool {
	return g&conditionAbstained > 0
}

func (g Condition) isCounted() bool {
	return g.isGood() || g.abstained()
}

func (g Condition) isGood() bool {
	return g == 0
}

func (g Condition) ignored() bool {
	return g&conditionBadBeacon > 0 || g&conditionVotesBeforeBase > 0
}

const (
	conditionBadBeacon Condition = 1 << iota
	conditionVotesBeforeBase
	conditionAbstained
	conditionNotConsistent
)
