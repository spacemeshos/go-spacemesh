package pendingtxs

import (
	"github.com/spacemeshos/go-spacemesh/common/types"
)

type meshProjector interface {
	GetProjection(addr types.Address, prevNonce, prevBalance uint64) (nonce, balance uint64, err error)
}

type poolProjector interface {
	GetProjection(addr types.Address, prevNonce, prevBalance uint64) (nonce, balance uint64)
}

// MeshAndPoolProjector provides nonce and balance projections based on unapplied transactions from the mesh and the
// mempool.
type MeshAndPoolProjector struct {
	mesh meshProjector
	pool poolProjector
}

// GetProjection returns a projected nonce and balance after applying transactions from the mesh and mempool, given the
// previous values. Errors can stem from database errors in the mesh (IO or deserialization errors).
func (p *MeshAndPoolProjector) GetProjection(addr types.Address, prevNonce, prevBalance uint64) (nonce, balance uint64, err error) {
	nonce, balance, err = p.mesh.GetProjection(addr, prevNonce, prevBalance)
	if err != nil {
		return 0, 0, err
	}
	nonce, balance = p.pool.GetProjection(addr, nonce, balance)
	return nonce, balance, nil
}

// NewMeshAndPoolProjector returns a new MeshAndPoolProjector.
func NewMeshAndPoolProjector(mesh meshProjector, pool poolProjector) *MeshAndPoolProjector {
	return &MeshAndPoolProjector{mesh: mesh, pool: pool}
}

type globalState interface {
	GetBalance(addr types.Address) uint64
	GetNonce(addr types.Address) uint64
}

// StateAndMeshProjector provides nonce and balance projections based on the global state and unapplied transactions on
// the mesh.
type StateAndMeshProjector struct {
	state globalState
	mesh  meshProjector
}

// NewStateAndMeshProjector returns a new StateAndMeshProjector.
func NewStateAndMeshProjector(state globalState, mesh meshProjector) *StateAndMeshProjector {
	return &StateAndMeshProjector{state: state, mesh: mesh}
}

// GetProjection returns a projected nonce and balance after applying transactions from the mesh on top of the global
// state. Errors can stem from database errors in the mesh (IO or deserialization errors).
func (p *StateAndMeshProjector) GetProjection(addr types.Address) (nonce, balance uint64, err error) {
	return p.mesh.GetProjection(addr, p.state.GetNonce(addr), p.state.GetBalance(addr))
}
