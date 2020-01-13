package timesync

import "time"

type ManualClock struct {
	*Ticker
	genesisTime time.Time
}

func (t *ManualClock) Tick() {
	t.onTick()
}

func NewManualClock(genesisTime time.Time) *ManualClock {
	t := &ManualClock{
		Ticker:      NewTicker(RealClock{}),
		genesisTime: genesisTime,
	}
	t.StartNotifying()
	return t
}

func (t ManualClock) GetGenesisTime() time.Time {
	return t.genesisTime
}
