package malfeasance

import (
	"github.com/spacemeshos/go-spacemesh/metrics"
)

const (
	validProofName   = "num_proofs"
	invalidProofName = "num_invalid_proofs"
	namespace        = "malfeasance"

	typeLabel = "type"
)

var (
	numProofs = metrics.NewCounter(
		validProofName,
		namespace,
		"number of malfeasance proofs",
		[]string{
			typeLabel,
		},
	)

	numInvalidProofs = metrics.NewCounter(
		invalidProofName,
		namespace,
		"number of invalid malfeasance proofs",
		[]string{
			typeLabel,
		},
	)

	numMalformed = numInvalidProofs.WithLabelValues("mal")
)
