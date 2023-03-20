package bootstrap

//go:generate scalegen -types InnerData,EpochData

type Update struct {
	Version   string    `json:"version"`
	Signature string    `json:"signature"`
	Data      InnerData `json:"data"`
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
