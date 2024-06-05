package malfeasance

import (
	"github.com/spacemeshos/go-spacemesh/metrics"
)

const (
	namespace = "malfeasance"

	typeLabel = "type"
)

var (
	numProofs = metrics.NewCounter(
		"num_proofs",
		namespace,
		"number of malfeasance proofs",
		[]string{
			typeLabel,
		},
	)

	numInvalidProofs = metrics.NewCounter(
		"num_invalid_proofs",
		namespace,
		"number of invalid malfeasance proofs",
		[]string{
			typeLabel,
		},
	)

	numMalformed = numInvalidProofs.WithLabelValues("mal")
)
