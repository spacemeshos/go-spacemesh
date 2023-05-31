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

type MsgType uint8

const (
	Preround MsgType = iota
	Propose
	Commit
	Notify
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
	ReceiveMsg(values [][]byte, round AbsRound, grade uint8) // (sid, r, v, d + 1 − s)
	// Increment round increments the current round
	IncRound() (v [][]byte, g uint8)
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
	var values [][]byte
	// Pass result to threshold gossip
	tgg.ReceiveMsg(values, r, g)
	return nil
}

type Protocol struct {
	iteration   int8
	hardLocked  bool
	lockedValue []byte
	values      [][]byte
	validValues [][][]byte
	round       AbsRound
	tgg TrhesholdGradedGossiper
}

func (p *Protocol) NextRound() *miniMsg {
	if p.round >= 0 && p.round <= 3 {
		p.validValues[p.round] = p.tgg.
	}
	switch p.round {
	case 0:
	case 1:
	case 2:
	case 3:
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
	values [][]byte
}

type AbsRound int8

func (r AbsRound) Type() MsgType {
	return MsgType(r % 7)
}

// To run this we just want a loop that pulls from 2 channels a timer channel and a channel of messages
