package wire

import (
	"context"

	"github.com/spacemeshos/go-spacemesh/common/types"
)

//go:generate mockgen -typed -package=wire -destination=./mocks.go -source=./interface.go

type postVerifier interface {
	PostV2Idx(
		ctx context.Context,
		smesherID types.NodeID,
		commitment types.ATXID,
		post *types.Post,
		challenge []byte,
		numUnits uint32,
		idx int,
	) error
}
