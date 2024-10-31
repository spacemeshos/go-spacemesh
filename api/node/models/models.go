package models

import (
	"encoding/hex"
	"fmt"

	"github.com/spacemeshos/go-spacemesh/common/types"
)

func ParseNodeID(hexID NodeID) (types.NodeID, error) {
	if len(hexID) != 2*len(types.NodeID{}) {
		return types.NodeID{}, fmt.Errorf("invalid node ID length: %d", len(hexID))
	}
	id, err := hex.DecodeString(hexID)
	if err != nil {
		return types.NodeID{}, fmt.Errorf("decoding node ID (%s): %w", hexID, err)
	}
	return types.BytesToNodeID(id), nil
}

func ParseATXID(hexID ATXID) (types.ATXID, error) {
	if len(hexID) != 2*len(types.ATXID{}) {
		return types.ATXID{}, fmt.Errorf("invalid atx ID length: %d", len(hexID))
	}
	id, err := hex.DecodeString(hexID)
	if err != nil {
		return types.ATXID{}, fmt.Errorf("decoding atx ID (%s): %w", hexID, err)
	}
	return types.BytesToATXID(id), nil
}

func ParseATX(atx *ActivationTx) (*types.ActivationTx, error) {
	smesherID, err := ParseNodeID(atx.SmesherID)
	if err != nil {
		return nil, err
	}

	atxID, err := ParseATXID(atx.ID)
	if err != nil {
		return nil, err
	}

	result := &types.ActivationTx{
		NumUnits:     atx.NumUnits,
		PublishEpoch: types.EpochID(atx.PublishEpoch),
		SmesherID:    smesherID,
		TickCount:    atx.TickCount,
		Weight:       atx.Weight,
	}
	if atx.Sequence != nil {
		result.Sequence = *atx.Sequence
	}
	result.SetID(atxID)

	return result, nil
}
