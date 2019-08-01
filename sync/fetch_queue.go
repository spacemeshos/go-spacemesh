package sync

import (
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/types"
)

type txFetchQueue struct {
	log.Log
	queue        chan types.TransactionId
	dependencies map[types.TransactionId][]func(res bool) error
}

type atxFetchQueue struct {
	log.Log
	queue        chan types.AtxId
	dependencies map[types.AtxId][]func(res bool) error
}

//todo work txs

//todo work atxs
