package types

import "go.uber.org/zap/zapcore"

type BallotTortoiseData struct {
	ID            BallotID       `json:"id"`
	Smesher       NodeID         `json:"node"`
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

func (b *BallotTortoiseData) MarshalLogObject(encoder zapcore.ObjectEncoder) error {
	encoder.AddString("id", b.ID.String())
	encoder.AddString("smesher", b.Smesher.ShortString())
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
	Beacon        Beacon `json:"beacon"`
	Eligibilities uint32 `json:"elig"`
}

func (r *ReferenceData) MarshalLogObject(encoder zapcore.ObjectEncoder) error {
	encoder.AddString("beacon", r.Beacon.String())
	encoder.AddUint32("elig", r.Eligibilities)
	return nil
}
