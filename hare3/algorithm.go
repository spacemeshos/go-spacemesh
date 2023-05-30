package hare3

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

type GradedGossiper interface {
	// GradedGossip takes the given sid (session id) value, key (verification
	// key) and grade and processes them. If a non nil v is returned the
	// originally received input message should be forwarded to all neighbors
	// and (sid, value, key, grade) is considered to have been output.
	GradedGossip(sid, value, key []byte, grade uint8) (v []byte)
}

type TrhesholdGradedGossiper interface {
	// Threshold graded gossip takes outputs from graded gossip, which are in
	// fact sets of values not single values, and returns sets of values that
	// have reached the required threshold, along with the grade for that set.
	ThresholdGradedGossip(sid, round uint32, value [][]byte, key []byte, grade uint8) (v [][]byte, g uint8) // (sid, r, v, d + 1 − s)
	// Increment round increments the current round
	IncRound()
}
