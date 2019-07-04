package monitoring

import (
	"time"
)

type Updater interface {
	Update()
}

type Monitor struct {
	interval time.Duration
	tracker  Updater
	term chan struct{}
}

func NewMonitor(refreshRate int, updater Updater, termChannel chan struct{}) *Monitor {
	m := new(Monitor)
	m.interval = time.Duration(refreshRate) * time.Second
	m.tracker = updater
	m.term = termChannel

	return m
}

func (m *Monitor) monitor() {
	for {
		m.tracker.Update()

		select {
		case <-m.term:
			return
		case <-time.After(m.interval):
		}

	}
}

func (m *Monitor) Start() {
	go m.monitor()
}



