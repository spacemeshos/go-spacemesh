package malfeasance

import (
	"github.com/spacemeshos/go-spacemesh/metrics"
)

const (
	namespace = "malfeasance"

	typeLabel = "type"

	multiATXs        = "atx"
	multiBallots     = "ballot"
	hareEquivocate   = "hare_eq"
	invalidPostIndex = "invalid_post_index"
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

	numProofsATX       = numProofs.WithLabelValues(multiATXs)
	numProofsBallot    = numProofs.WithLabelValues(multiBallots)
	numProofsHare      = numProofs.WithLabelValues(hareEquivocate)
	numProofsPostIndex = numProofs.WithLabelValues(invalidPostIndex)

	numInvalidProofs = metrics.NewCounter(
		"num_invalid_proofs",
		namespace,
		"number of invalid malfeasance proofs",
		[]string{
			typeLabel,
		},
	)

	numInvalidProofsATX       = numInvalidProofs.WithLabelValues(multiATXs)
	numInvalidProofsBallot    = numInvalidProofs.WithLabelValues(multiBallots)
	numInvalidProofsHare      = numInvalidProofs.WithLabelValues(hareEquivocate)
	numInvalidProofsPostIndex = numInvalidProofs.WithLabelValues(invalidPostIndex)
	numMalformed              = numInvalidProofs.WithLabelValues("mal")
)
