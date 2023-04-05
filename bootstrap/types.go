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
	ID     uint32      `json:"id"`
	Epochs []EpochData `json:"epochs"`
}

type EpochData struct {
	Epoch     uint32   `json:"epoch"`
	Beacon    string   `json:"beacon"`
	ActiveSet []string `json:"activeSet"`
}

type VerifiedUpdate struct {
	Persisted string
	ID        uint32
	Data      []*EpochOverride
}

type EpochOverride struct {
	Epoch     types.EpochID
	Beacon    types.Beacon
	ActiveSet []types.ATXID
}

func (vd *VerifiedUpdate) MarshalLogObject(encoder log.ObjectEncoder) error {
	encoder.AddString("persisted", vd.Persisted)
	for _, epoch := range vd.Data {
		encoder.AddString("beacon", epoch.Beacon.String())
		encoder.AddArray("activeset", log.ArrayMarshalerFunc(func(aencoder log.ArrayEncoder) error {
			for _, atx := range epoch.ActiveSet {
				aencoder.AppendString(atx.String())
			}
			return nil
		}))
	}
	return nil
}
