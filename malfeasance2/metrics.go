package malfeasance2

import "github.com/spacemeshos/go-spacemesh/metrics"

const (
	MetricNamespace        = "malfeasance2"
	MetricValidProofName   = "num_proofs"
	MetricInvalidProofName = "num_invalid_proofs"

	domainLabel = "domain"
	typeLabel   = "type"
)

var (
	numProofs = metrics.NewCounter(
		MetricValidProofName,
		MetricNamespace,
		"number of malfeasance proofs",
		[]string{
			domainLabel,
			typeLabel,
		},
	)

	numInvalidProofs = metrics.NewCounter(
		MetricInvalidProofName,
		MetricNamespace,
		"number of invalid malfeasance proofs",
		[]string{
			domainLabel,
			typeLabel,
		},
	)

	numMalformed = numInvalidProofs.WithLabelValues("mal", "unknown")
)
