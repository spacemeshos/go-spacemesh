package model

import (
	"math/rand"
	"testing"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/log/logtest"
)

func TestSanity(t *testing.T) {
	types.SetLayersPerEpoch(4)

	c := newCluster(logtest.New(t), rand.New(rand.NewSource(1001)))
	for i := 0; i < 2; i++ {
		c.addCore()
	}
	c.addHare().addBeacon()

	sanity := sanityRunner{cluster: c}
	for i := 0; i < 20; i++ {
		sanity.next()
	}
}

func TestFailure(t *testing.T) {
	types.SetLayersPerEpoch(4)

	rng := rand.New(rand.NewSource(1001))
	c := newCluster(logtest.New(t), rng)
	for i := 0; i < 50; i++ {
		c.addCore()
	}
	c.addHare().addBeacon()

	r := newFailingRunner(c, rng, [2]int{7, 100}).
		failable(EventBallot{}, EventBlock{}, EventAtx{})
	for i := 0; i < 9; i++ {
		r.next()
	}
}
