package monitoring

import (
	"time"
)

type recorder interface {
	Update()
	Status() string
	LogJSON()
}

// Monitor is a simple service to take samples
type Monitor struct {
	updateTicker *time.Ticker
	printTicker  *time.Ticker
	recorder     recorder
	term         chan struct{}
}

// NewMonitor constructs a new monitor instance
func NewMonitor(updateRate time.Duration, printRate time.Duration, updater recorder, termChannel chan struct{}) *Monitor {
	m := new(Monitor)
	m.updateTicker = time.NewTicker(updateRate)
	m.printTicker = time.NewTicker(printRate)
	m.recorder = updater
	m.term = termChannel

	return m
}

func (m *Monitor) monitor() {
	for {
		select {
		case <-m.term:
			m.updateTicker.Stop()
			m.printTicker.Stop()
			return
		case <-m.updateTicker.C:
			m.recorder.Update()
		case <-m.printTicker.C:
			//log.Info("%v", m.recorder.Status())
			m.recorder.LogJSON()
		}
	}
}

// Start monitoring
func (m *Monitor) Start() {
	go m.monitor()
}
