package activation

import (
	"github.com/spacemeshos/go-spacemesh/database"
	"github.com/spacemeshos/go-spacemesh/mesh"
	"github.com/spacemeshos/go-spacemesh/nipst"
	"testing"
)

type ActiveSetProviderMock struct{}

func (ActiveSetProviderMock) GetActiveSetSize(l mesh.LayerID) uint32 {
	return 10
}

type MeshProviderrMock struct{}

func (MeshProviderrMock) GetLatestView() []mesh.BlockID {
	return []mesh.BlockID{1, 2, 3}
}

func (MeshProviderrMock) LatestLayerId() mesh.LayerID {
	return 7
}

type NetMock struct{}

func (NetMock) Broadcast(id string, d []byte) error {
	return nil
}

func TestBuilder_BuildActivationTx(t *testing.T) {
	//todo: implement test
	id := mesh.Id{}
	b := NewBuilder(id, &database.MemDatabase{}, NetMock{}, ActiveSetProviderMock{}, MeshProviderrMock{})
	b.BuildActivationTx(nipst.NIPST{})
}
