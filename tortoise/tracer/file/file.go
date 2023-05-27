package file

import (
	"os"

	"github.com/spacemeshos/go-spacemesh/tortoise/tracer"
)

var _ tracer.Tracer = (*FileTracer)(nil)

type FileTracer struct {
	f os.File
}
