package model

import (
	"math/rand"
	"reflect"
	"strconv"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/log"
)

type model interface {
	OnEvent(Event) []Event
}

func newCluster(logger log.Log, rng *rand.Rand) *cluster {
	return &cluster{logger: logger, rng: rng}
}

type cluster struct {
	rng    *rand.Rand
	logger log.Log
	models []model
}

func (r *cluster) nextid() string {
	return strconv.Itoa(len(r.models))
}

func (r *cluster) add(m model) *cluster {
	r.models = append(r.models, m)
	return r
}

func (r *cluster) addCore() *cluster {
	return r.add(newCore(r.rng, r.logger.Named("core-"+r.nextid())))
}

func (r *cluster) addHare() *cluster {
	return r.add(newHare(r.rng))
}

func (r *cluster) addBeacon() *cluster {
	return r.add(newBeacon(r.rng))
}

func (r *cluster) iterate(f func(m model)) {
	for _, m := range r.models {
		f(m)
	}
}

// sanityRunner processes events sequentially without any unexpected behavior.
type sanityRunner struct {
	cluster *cluster
	lid     types.LayerID
}

func (r *sanityRunner) next() {
	r.lid = r.lid.Add(1)
	r.drainEvents(EventLayerStart{LayerID: r.lid})
	r.drainEvents(EventLayerEnd{LayerID: r.lid})
}

func (r *sanityRunner) drainEvents(events ...Event) {
	for len(events) > 0 {
		var next []Event
		r.cluster.iterate(func(m model) {
			for _, ev := range events {
				for _, output := range m.OnEvent(ev) {
					next = append(next, output)
				}
			}
		})
		events = next
	}
}

func newFailingRunner(c *cluster, rng *rand.Rand, probability [2]int) *failingRunner {
	return &failingRunner{
		cluster:     c,
		rng:         rng,
		probability: probability,
	}
}

type failingRunner struct {
	cluster     *cluster
	probability [2]int
	rng         *rand.Rand
	lid         types.LayerID
	failables   map[reflect.Type]struct{}
}

func (r *failingRunner) next() {
	r.lid = r.lid.Add(1)
	r.drainEvents(EventLayerStart{LayerID: r.lid})
	r.drainEvents(EventLayerEnd{LayerID: r.lid})
}

func (r *failingRunner) failable(ev Event) *failingRunner {
	if r.failables == nil {
		r.failables = map[reflect.Type]struct{}{}
	}
	r.failables[reflect.TypeOf(ev)] = struct{}{}
	return r
}

func (r *failingRunner) isFailable(ev Event) bool {
	if r.failables == nil {
		return true
	}
	_, exist := r.failables[reflect.TypeOf(ev)]
	return exist
}

func (r *failingRunner) drainEvents(events ...Event) {
	for len(events) > 0 {
		var next []Event
		r.cluster.iterate(func(m model) {
			for _, ev := range events {
				if r.isFailable(ev) && r.probability[0] > rand.Intn(r.probability[1]) {
					continue
				}
				for _, output := range m.OnEvent(ev) {
					next = append(next, output)
				}
			}
		})
		events = next
	}
}
