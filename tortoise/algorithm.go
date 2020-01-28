package tortoise

import (
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/mesh"
	"sync"
)

type Algorithm struct {
	Tortoise
	sync.Mutex
}

type Tortoise interface {
	handleIncomingLayer(ll *types.Layer)
	LatestComplete() types.LayerID
}

func NewAlgorithm(layerSize int, mdb *mesh.MeshDB, hdist int, lg log.Log) *Algorithm {
	alg := &Algorithm{Tortoise: NewNinjaTortoise(layerSize, mdb, hdist, lg)}
	alg.HandleIncomingLayer(mesh.GenesisLayer())
	return alg
}

func NewRecoveredAlgorithm(mdb *mesh.MeshDB, lg log.Log) *Algorithm {
	trtl := &NinjaTortoise{Log: lg, Mesh: mdb}

	ni, err := trtl.RecoverTortoise()
	if err != nil {
		lg.Panic("could not recover tortoise state from disc ", err)
	}

	lg.Info("recovered tortoise from disc")
	trtl.ninjaTortoise = ni.(*ninjaTortoise)

	alg := &Algorithm{Tortoise: trtl}

	return alg
}

func (alg *Algorithm) HandleLateBlock(b *types.Block) (types.LayerID, types.LayerID) {
	//todo feed all layers from b's layer to tortoise
	l := types.NewLayer(b.Layer())
	l.AddBlock(b)
	oldPbase, newPbase := alg.HandleIncomingLayer(l)
	log.With().Info("late block ", log.LayerId(uint64(b.Layer())), log.BlockId(b.Id().String()))
	return oldPbase, newPbase
}

func (alg *Algorithm) HandleIncomingLayer(ll *types.Layer) (types.LayerID, types.LayerID) {
	alg.Lock()
	defer alg.Unlock()
	oldPbase := alg.LatestComplete()
	alg.Tortoise.handleIncomingLayer(ll)
	newPbase := alg.LatestComplete()
	updateMetrics(alg, ll)
	return oldPbase, newPbase
}

func updateMetrics(alg *Algorithm, ll *types.Layer) {
	pbaseCount.Set(float64(alg.LatestComplete()))
	processedCount.Set(float64(ll.Index()))
}
