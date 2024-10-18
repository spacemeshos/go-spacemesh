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

func (rsr *RangeSetReconciler) DoRound(s Sender) (done bool, err error) { return rsr.doRound(s) }
func (rsr *RangeSetReconciler) Set() OrderedSet                         { return rsr.os }
