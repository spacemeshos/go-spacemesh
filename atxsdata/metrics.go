package atxsdata

import "github.com/spacemeshos/go-spacemesh/metrics"

var atxsCounter = metrics.NewCounter(
	"atxs",
	"consensus_cache",
	"number of atxs",
	[]string{"epoch"},
)
