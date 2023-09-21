package cache

import "github.com/spacemeshos/go-spacemesh/metrics"

var (
	atxsCounter = metrics.NewCounter(
		"atxs",
		"consensus_cache",
		"number of atxs",
		[]string{"epoch"},
	)
	identitiesCounter = metrics.NewCounter(
		"identities",
		"consensus_cache",
		"number of identities",
		[]string{"epoch"},
	)
)
