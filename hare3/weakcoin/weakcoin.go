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
// The weak coin is something that should be agreed upon by all honest parties,
// to achieve this all parties share their signatures and then the signature
// that has the lowest value is accepted as the coin to use.
//
// To actually get a coin value from the signature we look at the least
// significant bit.
package weakcoin

import (
	"errors"
	"sort"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"golang.org/x/exp/maps"
)

type Chooser struct {
	coins map[types.NodeID]types.VrfSignature
}

func (c *Chooser) Add(id types.NodeID, coin types.VrfSignature) {
	c.coins[id] = coin
}

func (c *Chooser) Remove(id types.NodeID) {
	delete(c.coins, id)
}

func (c *Chooser) Choose() (bool, error) {
	if len(c.coins) == 0 {
		return false, errors.New("no coins to choose from")
	}
	vals := maps.Values(c.coins)
	sort.Slice(vals, func(i, j int) bool {
		return vals[i].Cmp(&vals[j]) == -1
	})
	return vals[0].LSB() == 1, nil
}
