package txs

import (
	"math/rand/v2"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/log"
)

// ShuffleWithNonceOrder perform a Fisher-Yates shuffle on the transactions.
// note that after shuffling, the original list of transactions are no longer in nonce order
// within the same principal. we simply check which principal occupies the spot after
// the shuffle and retrieve their transactions in nonce order.
func ShuffleWithNonceOrder(
	logger log.Log,
	rng *rand.Rand,
	numTXs int,
	ntxs []*NanoTX,
	byAddrAndNonce map[types.Address][]*NanoTX,
) []types.TransactionID {
	rng.Shuffle(len(ntxs), func(i, j int) { ntxs[i], ntxs[j] = ntxs[j], ntxs[i] })
	total := min(len(ntxs), numTXs)
	result := make([]types.TransactionID, 0, total)
	packed := make(map[types.Address][]uint64)
	for _, ntx := range ntxs[:total] {
		// if a spot is taken by a principal, we add its TX for the next eligible nonce
		p := ntx.Principal
		if _, ok := byAddrAndNonce[p]; !ok {
			logger.With().Fatal("principal missing", p)
		}
		if len(byAddrAndNonce[p]) == 0 {
			logger.With().Fatal("txs missing", p)
		}
		toAdd := byAddrAndNonce[p][0]
		result = append(result, toAdd.ID)
		if _, ok := packed[p]; !ok {
			packed[p] = []uint64{toAdd.Nonce, toAdd.Nonce}
		} else {
			packed[p][1] = toAdd.Nonce
		}
		if len(byAddrAndNonce[p]) == 1 {
			delete(byAddrAndNonce, p)
		} else {
			byAddrAndNonce[p] = byAddrAndNonce[p][1:]
		}
	}
	logger.With().Debug("packed txs", log.Array("ranges", log.ArrayMarshalerFunc(func(encoder log.ArrayEncoder) error {
		for addr, nonces := range packed {
			_ = encoder.AppendObject(log.ObjectMarshallerFunc(func(encoder log.ObjectEncoder) error {
				encoder.AddString("addr", addr.String())
				encoder.AddUint64("from", nonces[0])
				encoder.AddUint64("to", nonces[1])
				return nil
			}))
		}
		return nil
	})))
	return result
}
