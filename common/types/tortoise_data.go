package types

import "github.com/spacemeshos/go-spacemesh/log"

type AtxTortoiseData struct {
	ID          ATXID   `json:"id"`
	Smesher     NodeID  `json:"node"`
	TargetEpoch EpochID `json:"target"`
	BaseHeight  uint64  `json:"base"`
	Height      uint64  `json:"height"`
	Weight      uint64  `json:"weight"`
}

type BallotTortoiseData struct {
	ID            BallotID       `json:"id"`
	Layer         LayerID        `json:"lid"`
	Eligibilities uint32         `json:"elig"`
	AtxID         ATXID          `json:"atxid"`
	Opinion       Opinion        `json:"opinion"`
	EpochData     *ReferenceData `json:"epochdata"`
	Ref           *BallotID      `json:"ref"`
	Malicious     bool           `json:"mal"`
}

func (b *BallotTortoiseData) SetMalicious() {
	b.Malicious = true
}

func (b *BallotTortoiseData) MarshalLogObject(encoder log.ObjectEncoder) error {
	encoder.AddString("id", b.ID.String())
	encoder.AddUint32("layer", b.Layer.Uint32())
	encoder.AddString("atxid", b.AtxID.String())
	encoder.AddObject("opinion", &b.Opinion)
	encoder.AddUint32("elig", b.Eligibilities)
	if b.EpochData != nil {
		encoder.AddObject("epochdata", b.EpochData)
	} else if b.Ref != nil {
		encoder.AddString("ref", b.Ref.String())
	}
	encoder.AddBool("malicious", b.Malicious)
	return nil
}

type ReferenceData struct {
	Beacon        Beacon  `json:"beacon"`
	ActiveSet     []ATXID `json:"set"`
	Eligibilities uint32  `json:"elig"`
}

func (r *ReferenceData) MarshalLogObject(encoder log.ObjectEncoder) error {
	encoder.AddString("beacon", r.Beacon.String())
	encoder.AddInt("set size", len(r.ActiveSet))
	encoder.AddUint32("elig", r.Eligibilities)
	return nil
}
