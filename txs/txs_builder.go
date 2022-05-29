package txs

import (
	"math/rand"
	"time"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/common/util"
	"github.com/spacemeshos/go-spacemesh/log"
	txtypes "github.com/spacemeshos/go-spacemesh/txs/types"
)

func getProposalTXs(logger log.Log, numTXs int, predictedBlock []*txtypes.NanoTX, byAddrAndNonce map[types.Address][]*txtypes.NanoTX) []types.TransactionID {
	if len(predictedBlock) <= numTXs {
		result := make([]types.TransactionID, 0, len(predictedBlock))
		for _, ntx := range predictedBlock {
			result = append(result, ntx.Tid)
		}
		return result
	}
	// randomly select transactions from the predicted block.
	rng := rand.New(rand.NewSource(time.Now().UnixNano()))
	return shuffleWithNonceOrder(logger, rng, numTXs, predictedBlock, byAddrAndNonce)
}

// perform a Fisher-Yates shuffle on the transactions. note that after shuffling, the original list of transactions
// are no longer in nonce order within the same principal. we simply check which principal occupies the spot after
// the shuffle and retrieve their transactions in nonce order.
func shuffleWithNonceOrder(
	logger log.Log,
	rng *rand.Rand,
	numTXs int,
	ntxs []*txtypes.NanoTX,
	byAddrAndNonce map[types.Address][]*txtypes.NanoTX,
) []types.TransactionID {
	rng.Shuffle(len(ntxs), func(i, j int) { ntxs[i], ntxs[j] = ntxs[j], ntxs[i] })
	total := util.Min(len(ntxs), numTXs)
	result := make([]types.TransactionID, 0, total)
	for _, ntx := range ntxs[:total] {
		// if a spot is taken by a principal, we add its TX for the next eligible nonce
		p := ntx.Principal
		if _, ok := byAddrAndNonce[p]; !ok {
			logger.With().Fatal("principal missing", p)
		}
		if len(byAddrAndNonce[p]) == 0 {
			logger.With().Fatal("txs missing", p)
		}
		result = append(result, byAddrAndNonce[p][0].Tid)
		if len(byAddrAndNonce[p]) == 1 {
			delete(byAddrAndNonce, p)
		} else {
			byAddrAndNonce[p] = byAddrAndNonce[p][1:]
		}
	}
	return result
}
