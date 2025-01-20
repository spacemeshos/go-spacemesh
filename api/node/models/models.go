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

func ParseHash20(hashHex Hash20) (types.Hash20, error) {
	if len(hashHex) != 2*len(types.Hash20{}) {
		return types.Hash20{}, fmt.Errorf("invalid Hash20 ID length: %d", len(hashHex))
	}
	id, err := hex.DecodeString(hashHex)
	if err != nil {
		return types.Hash20{}, fmt.Errorf("decoding Hash20 ID (%s): %w", hashHex, err)
	}
	return types.Hash20(id), nil
}

func ParseHash32(hashHex Hash32) (types.Hash32, error) {
	if len(hashHex) != 2*len(types.Hash32{}) {
		return types.Hash32{}, fmt.Errorf("invalid Hash32 ID length: %d", len(hashHex))
	}
	id, err := hex.DecodeString(hashHex)
	if err != nil {
		return types.Hash32{}, fmt.Errorf("decoding Hash32 ID (%s): %w", hashHex, err)
	}
	return types.Hash32(id), nil
}

func ParseBeacon(beaconHex Beacon) (types.Beacon, error) {
	if len(beaconHex) != 2*len(types.Beacon{}) {
		return types.Beacon{}, fmt.Errorf("beacon length must be 8 (was: %d)", len(beaconHex))
	}
	beacon, err := hex.DecodeString(beaconHex)
	if err != nil {
		return types.Beacon{}, fmt.Errorf("decoding beacon: %w", err)
	}
	return types.Beacon(beacon), nil
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

func ParseTransactionIDs(ids []Hash32) ([]types.TransactionID, error) {
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
