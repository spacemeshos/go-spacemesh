// Package weakcoin provides a means to calculate the weakcoin that is required
// by the tortoise protocol to decide how to vote for blocks that do not have a
// clear majority of votes.
//
// The weak coin for a node is calculated by using a VRF (verifiable random
// function) in this case it is achieved by having nodes sign over some nonce
// that is out of their control (such as layer+round, the specifics of the
// actual implementation may change) and then interpreting the resultant
// signature. It is verifiable since the signature can be verified with the
// public key of the creator.
//
// To find the coin all parties share their VRF and the one with the lowest
// value is chosen, the coin value is determined by looking at the least
// significant bit of the chosen VRF.
//
// The meaning of weak here is that the coin toss only needs to be "good"
// (unbiased, unpredictable and in full consensus among honest parties) with
// probability p > 0. I.E. eventually there is a "good" toss.
package weakcoin

import (
	"sync"

	"github.com/spacemeshos/go-spacemesh/common/types"
)

type Chooser struct {
	coin *types.VrfSignature
	mu   sync.Mutex
}

func NewChooser() *Chooser { return &Chooser{} }

// Put puts the coin into the chooser.
func (c *Chooser) Put(coin *types.VrfSignature) {
	c.mu.Lock()
	defer c.mu.Unlock()
	switch {
	case c.coin == nil:
		c.coin = coin
	case coin.Cmp(c.coin) == -1:
		// if the given coin is smaller than the provided coin then store it.
		c.coin = coin
	}
}

// Choose returns the coin value of the lowest seen coin or nil if no coins
// have been seen.
func (c *Chooser) Choose() *bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.coin == nil {
		return nil
	}
	result := c.coin.LSB() == 1
	return &result
}
