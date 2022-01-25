package model

import (
	"math/rand"
	"testing"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/log/logtest"
)

func TestBasicModel(t *testing.T) {
	types.SetLayersPerEpoch(4)
	const total = 20

	rng := rand.New(rand.NewSource(1001))
	c := newCluster(logtest.New(t), rng)
	for i := 0; i < 50; i++ {
		c.addCore()
	}
	c.addHare().addBeacon()

	msgr := reliableMessenger{}
	monitor := newVerifiedMonitor(t, types.GetEffectiveGenesis())

	r := newFailingRunner(c, &msgr, []Monitor{monitor}, rng, [2]int{5, 100}).
		failable(MessageBallot{})
	for i := 0; i < total; i++ {
		r.next()
		monitor.Test()
	}
}
