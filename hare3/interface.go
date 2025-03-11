package hare3

import (
	"context"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/signing"
)

//go:generate mockgen -typed -package=hare3 -destination=./mocks.go -source=./interface.go

type oracle interface {
	Validate(context.Context, types.LayerID, uint32, int, types.NodeID, types.VrfSignature, uint16) (bool, error)
	CalcEligibility(context.Context, types.LayerID, uint32, int, types.NodeID, types.VrfSignature) (uint16, error)
}

type NodeService interface {
	BlockID(ctx context.Context, layer types.LayerID) (types.BlockID, error)
	HareRoundTemplate(ctx context.Context, layer types.LayerID, round IterRound) (*Body, error)
	Publish(ctx context.Context, proto string, blob []byte) error
}

type certifier interface {
	CertifyBlock(
		ctx context.Context,
		s *signing.EdSigner,
		lid types.LayerID,
		bid types.BlockID,
		beacon types.Beacon,
	) error
}
