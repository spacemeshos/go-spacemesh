package rangesync

var (
	StartWireConduit = startWireConduit
	StringToFP       = stringToFP
	CHash            = chash
)

type (
	Sender  = sender
	DumbSet = dumbSet
)

func DoRound(rsr *RangeSetReconciler, s Sender) (done bool, err error) { return rsr.doRound(s) }
func ReconcilerOrderedSet(rsr *RangeSetReconciler) OrderedSet          { return rsr.os }
