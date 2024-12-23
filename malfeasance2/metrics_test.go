package malfeasance2

import "github.com/prometheus/client_golang/prometheus"

func NumValidProofs() *prometheus.CounterVec {
	return numProofs
}

func NumInvalidProofs() *prometheus.CounterVec {
	return numInvalidProofs
}
