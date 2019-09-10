package pending_txs

import (
	"github.com/spacemeshos/go-spacemesh/common/types"
)

type meshProjector interface {
	GetProjection(addr types.Address, prevNonce, prevBalance uint64) (nonce, balance uint64, err error)
}

type poolProjector interface {
	GetProjection(addr types.Address, prevNonce, prevBalance uint64) (nonce, balance uint64)
}

type MeshAndPoolProjector struct {
	mesh meshProjector
	pool poolProjector
}

func (p *MeshAndPoolProjector) GetProjection(addr types.Address, prevNonce, prevBalance uint64) (nonce, balance uint64, err error) {
	nonce, balance, err = p.mesh.GetProjection(addr, prevNonce, prevBalance)
	if err != nil {
		return 0, 0, err
	}
	nonce, balance = p.pool.GetProjection(addr, nonce, balance)
	return nonce, balance, nil
}

func NewMeshAndPoolProjector(mesh meshProjector, pool poolProjector) *MeshAndPoolProjector {
	return &MeshAndPoolProjector{mesh: mesh, pool: pool}
}

type globalState interface {
	GetBalance(addr types.Address) uint64
	GetNonce(addr types.Address) uint64
}

type StateAndMeshProjector struct {
	state globalState
	mesh  meshProjector
}

func NewStateAndMeshProjector(state globalState, mesh meshProjector) *StateAndMeshProjector {
	return &StateAndMeshProjector{state: state, mesh: mesh}
}

func (p *StateAndMeshProjector) GetProjection(addr types.Address) (nonce, balance uint64, err error) {
	return p.mesh.GetProjection(addr, p.state.GetNonce(addr), p.state.GetBalance(addr))
}
