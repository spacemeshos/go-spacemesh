package malfeasance2

import "github.com/spacemeshos/go-spacemesh/metrics"

const (
	namespace = "malfeasance2"

	domainLabel = "domain"
	typeLabel   = "type"
)

var (
	numProofs = metrics.NewCounter(
		"num_proofs",
		namespace,
		"number of malfeasance proofs",
		[]string{
			domainLabel,
			typeLabel,
		},
	)

	numInvalidProofs = metrics.NewCounter(
		"num_invalid_proofs",
		namespace,
		"number of invalid malfeasance proofs",
		[]string{
			domainLabel,
			typeLabel,
		},
	)

	numMalformed = numInvalidProofs.WithLabelValues("mal", "unknown")
)
