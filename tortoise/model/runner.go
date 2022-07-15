package model

import (
	"math/rand"
	"reflect"
	"strconv"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/log"
)

type model interface {
	OnMessage(Messenger, Message)
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
	id := r.nextid()
	return r.add(newCore(r.rng, id, r.logger.Named("core-"+id)))
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

func newFailingRunner(c *cluster,
	messenger Messenger,
	monitors []Monitor,
	rng *rand.Rand,
	probability [2]int,
) *failingRunner {
	return &failingRunner{
		cluster:     c,
		messenger:   messenger,
		monitors:    monitors,
		rng:         rng,
		probability: probability,
	}
}

type failingRunner struct {
	cluster   *cluster
	messenger Messenger
	monitors  []Monitor
	// probability that messages in failables will fail by the model
	probability [2]int
	rng         *rand.Rand
	lid         types.LayerID
	failables   map[reflect.Type]struct{}
}

func (r *failingRunner) next() {
	r.lid = r.lid.Add(1)
	r.messenger.Send(MessageLayerStart{LayerID: r.lid})
	r.consume()
	r.messenger.Send(MessageLayerEnd{LayerID: r.lid})
	r.consume()
}

func (r *failingRunner) failable(events ...Message) *failingRunner {
	if r.failables == nil {
		r.failables = map[reflect.Type]struct{}{}
	}
	for _, ev := range events {
		r.failables[reflect.TypeOf(ev)] = struct{}{}
	}
	return r
}

func (r *failingRunner) isFailable(ev Message) bool {
	if r.failables == nil {
		return true
	}
	_, exist := r.failables[reflect.TypeOf(ev)]
	return exist
}

func (r *failingRunner) consume() {
	for msg := r.messenger.PopMessage(); msg != nil; msg = r.messenger.PopMessage() {
		if r.isFailable(msg) && r.probability[0] > r.rng.Intn(r.probability[1]) {
			continue
		}
		r.cluster.iterate(func(m model) {
			m.OnMessage(r.messenger, msg)
		})
	}
	for ev := r.messenger.PopEvent(); ev != nil; ev = r.messenger.PopEvent() {
		for _, monitor := range r.monitors {
			monitor.OnEvent(ev)
		}
	}
}
