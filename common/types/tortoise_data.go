package types

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
	AtxID         ATXID          `json:"atxid"`
	Smesher       NodeID         `json:"node"`
	Opinion       Opinion        `json:"opinion"`
	Eligibilities uint64         `json:"elig"`
	EpochData     *ReferenceData `json:"refdata"`
	Pointer       *BallotID      `json:"ref"`
	Malicious     bool           `json:"mal"`
}

type ReferenceData struct {
	Beacon        Beacon  `json:"beacon"`
	ActiveSet     []ATXID `json:"set"`
	Eligibilities uint64  `json:"elig"`
}
