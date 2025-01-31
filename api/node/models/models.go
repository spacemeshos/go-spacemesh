package models

import (
	"fmt"

	"github.com/spacemeshos/go-spacemesh/common/types"
)

func ParseNodeID(id Bytes32) (types.NodeID, error) {
	if len(id) != len(types.NodeID{}) {
		return types.NodeID{}, fmt.Errorf("invalid node ID length: %d", len(id))
	}
	return types.BytesToNodeID(id), nil
}

func ParseATXID(id Bytes32) (types.ATXID, error) {
	if len(id) != len(types.ATXID{}) {
		return types.ATXID{}, fmt.Errorf("invalid atx ID length: %d", len(id))
	}
	return types.BytesToATXID(id), nil
}

func ParseHash20(h Bytes20) (types.Hash20, error) {
	if len(h) != len(types.Hash20{}) {
		return types.Hash20{}, fmt.Errorf("invalid Hash20 ID length: %d", len(h))
	}
	return types.Hash20(h), nil
}

func ParseHash32(h Bytes20) (types.Hash32, error) {
	if len(h) != len(types.Hash32{}) {
		return types.Hash32{}, fmt.Errorf("invalid Hash32 ID length: %d", len(h))
	}
	return types.Hash32(h), nil
}

func ParseBeacon(b Beacon) (types.Beacon, error) {
	if len(b) != len(types.Beacon{}) {
		return types.Beacon{}, fmt.Errorf("beacon length must be 4 (was: %d)", len(b))
	}
	return types.Beacon(b), nil
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

func ParseVotes(votes []Vote) ([]types.Vote, error) {
	if len(votes) == 0 {
		return nil, nil
	}
	decoded := make([]types.Vote, 0, len(votes))
	for _, v := range votes {
		blockID, err := ParseHash20(v.ID)
		if err != nil {
			return nil, fmt.Errorf("decoding vote blockID: %w", err)
		}
		decoded = append(decoded, types.Vote{
			ID:      types.BlockID(blockID),
			LayerID: types.LayerID(v.LayerID),
			Height:  v.Height,
		})
	}
	return decoded, nil
}

func ParseLayers(lids []LayerID) []types.LayerID {
	if len(lids) == 0 {
		return nil
	}
	decoded := make([]types.LayerID, 0, len(lids))
	for _, l := range lids {
		decoded = append(decoded, types.LayerID(l))
	}
	return decoded
}

func ParseTransactionIDs(ids []Bytes32) ([]types.TransactionID, error) {
	if len(ids) == 0 {
		return nil, nil
	}
	decoded := make([]types.TransactionID, 0, len(ids))
	for _, id := range ids {
		id, err := ParseHash32(id)
		if err != nil {
			return nil, fmt.Errorf("decoding TX ID: %w", err)
		}
		decoded = append(decoded, types.TransactionID(id))
	}
	return decoded, nil
}
