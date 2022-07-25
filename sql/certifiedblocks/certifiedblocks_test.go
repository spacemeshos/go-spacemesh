package certifiedblocks_test

import (
	"fmt"
	"github.com/spacemeshos/go-spacemesh/codec"
	"testing"
)

type structWithUnexportedFields struct {
	value int
}

func TestThingy(t *testing.T) {
	thing := structWithUnexportedFields{
		value: 32,
	}
	bytes, _ := codec.Encode(thing)
	fmt.Printf("struct length: %v", len(bytes))
}
