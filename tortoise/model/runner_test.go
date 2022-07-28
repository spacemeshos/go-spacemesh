package model

import (
	"math/rand"
	"testing"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/log/logtest"
)

func TestBasicModel(t *testing.T) {
	types.SetLayersPerEpoch(4)
	const (
		numLayers   = 20
		numSmeshers = 50
	)

	rng := rand.New(rand.NewSource(1001))
	c := newCluster(logtest.New(t), rng)
	for i := 0; i < numSmeshers; i++ {
		c.addCore()
	}
	c.addHare().addBeacon()

	msgr := reliableMessenger{}
	monitor := newVerifiedMonitor(t, types.GetEffectiveGenesis())

	r := newFailingRunner(c, &msgr, []Monitor{monitor}, rng, [2]int{5, 100}).
		failable(MessageBallot{})
	for i := 0; i < numLayers; i++ {
		r.next()
		monitor.Test()
	}
}
