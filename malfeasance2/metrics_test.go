package malfeasance2

import "github.com/prometheus/client_golang/prometheus"

func (h *Handler) NumValidProofs() *prometheus.CounterVec {
	return h.numProofs
}

func (h *Handler) NumInvalidProofs() *prometheus.CounterVec {
	return h.numInvalidProofs
}

func (h *Handler) NumMalProof() prometheus.Counter {
	return h.numMalformed
}
