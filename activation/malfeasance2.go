package activation

import (
	"context"
	"fmt"

	"github.com/spacemeshos/go-spacemesh/activation/wire"
	"github.com/spacemeshos/go-spacemesh/codec"
	"github.com/spacemeshos/go-spacemesh/common/types"
)

type MalfeasanceHandlerV2 struct{}

func NewMalfeasanceHandlerV2() *MalfeasanceHandlerV2 {
	return &MalfeasanceHandlerV2{}
}

func (mh *MalfeasanceHandlerV2) decodeProof(data []byte) (wire.Proof, error) {
	var atxProof wire.ATXProof
	if err := codec.Decode(data, &atxProof); err != nil {
		return nil, err
	}

	proof, err := atxProof.Decode()
	if err != nil {
		return nil, err
	}
	return proof, nil
}

func (mh *MalfeasanceHandlerV2) Info(data []byte) (map[string]string, error) {
	proof, err := mh.decodeProof(data)
	if err != nil {
		return nil, fmt.Errorf("decoding ATX malfeasance proof: %w", err)
	}
	info := proof.Info()
	info["type"] = proof.String()
	return info, nil
}

func (p *MalfeasanceHandlerV2) Publish(ctx context.Context, id types.NodeID, proof wire.Proof) error {
	// TODO(mafa): implement me
	return nil
}
