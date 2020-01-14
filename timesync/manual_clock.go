package timesync

import "time"

type ManualClock struct {
	*Ticker
	genesisTime time.Time
}

func (t *ManualClock) Tick() {
	t.Notify()
}

func NewManualClock(genesisTime time.Time) *ManualClock {
	t := &ManualClock{
		Ticker:      NewTicker(RealClock{}, LayerConv{genesis: genesisTime}),
		genesisTime: genesisTime,
	}
	t.StartNotifying()
	return t
}

func (t ManualClock) GetGenesisTime() time.Time {
	return t.genesisTime
}

func (t ManualClock) Close() {
}
