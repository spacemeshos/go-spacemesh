package malfeasance2

import "github.com/spacemeshos/go-spacemesh/metrics"

const (
	namespace        = "malfeasance2"
	validProofName   = "num_proofs"
	invalidProofName = "num_invalid_proofs"

	domainLabel = "domain"
	typeLabel   = "type"
)

var (
	numProofs = metrics.NewCounter(
		validProofName,
		namespace,
		"number of malfeasance proofs",
		[]string{
			domainLabel,
			typeLabel,
		},
	)

	numInvalidProofs = metrics.NewCounter(
		invalidProofName,
		namespace,
		"number of invalid malfeasance proofs",
		[]string{
			domainLabel,
			typeLabel,
		},
	)

	numMalformed = numInvalidProofs.WithLabelValues("mal", "unknown")
)
