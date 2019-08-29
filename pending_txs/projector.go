package pending_txs

import "github.com/spacemeshos/go-spacemesh/common/types"

type meshProjector interface {
	GetProjection(addr types.Address, prevNonce, prevBalance uint64) (nonce, balance uint64, err error)
}

type poolProjector interface {
	GetProjection(addr types.Address, prevNonce, prevBalance uint64) (nonce, balance uint64)
}

type Projector struct {
	mesh meshProjector
	pool poolProjector
}

func (p *Projector) GetProjection(addr types.Address, prevNonce, prevBalance uint64) (nonce, balance uint64, err error) {
	nonce, balance, err = p.mesh.GetProjection(addr, prevNonce, prevBalance)
	if err != nil {
		return 0, 0, err
	}
	nonce, balance = p.pool.GetProjection(addr, nonce, balance)
	return nonce, balance, nil
}

func NewProjector(mesh meshProjector, pool poolProjector) *Projector {
	return &Projector{mesh: mesh, pool: pool}
}
