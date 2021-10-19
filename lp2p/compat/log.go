package compat

import (
	"sync"

	ipfslogwriter "github.com/ipfs/go-log/writer"
)

var once sync.Once

// CloseWriter closes ipfs/go-log background writer once.
func CloseWriter() {
	once.Do(func() {
		ipfslogwriter.WriterGroup.Close()
	})
}
