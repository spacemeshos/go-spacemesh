package bootstrap

import (
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/log"
)

type Update struct {
	Version string    `json:"version"`
	Data    InnerData `json:"data"`
}

type InnerData struct {
	Epoch EpochData `json:"epoch"`
}

type EpochData struct {
	ID        uint32   `json:"number"`
	Beacon    string   `json:"beacon"`
	ActiveSet []string `json:"activeSet"`
}

type VerifiedUpdate struct {
	Data      *EpochOverride
	Persisted string
}

type EpochOverride struct {
	Epoch     types.EpochID
	Beacon    types.Beacon
	ActiveSet []types.ATXID
}

func (vd *VerifiedUpdate) MarshalLogObject(encoder log.ObjectEncoder) error {
	encoder.AddString("persisted", vd.Persisted)
	encoder.AddString("epoch", vd.Data.Epoch.String())
	encoder.AddString("beacon", vd.Data.Beacon.String())
	encoder.AddInt("activeset_size", len(vd.Data.ActiveSet))
	encoder.AddArray("activeset", log.ArrayMarshalerFunc(func(aencoder log.ArrayEncoder) error {
		for _, atx := range vd.Data.ActiveSet {
			aencoder.AppendString(atx.String())
		}
		return nil
	}))
	return nil
}
