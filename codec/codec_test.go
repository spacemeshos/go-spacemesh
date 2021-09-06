package codec

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

type testStruct struct {
	Buf  []byte
	Int1 uint64
	Int2 uint64
	Str  string
}

func BenchmarkEncode(b *testing.B) {
	value := testStruct{
		Buf:  make([]byte, 128),
		Int1: 1010231312,
		Int2: 321321321312,
		Str:  strings.Repeat("test", 10),
	}

	b.ReportAllocs()
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		_, err := Encode(value)
		if err != nil {
			require.NoError(b, err)
		}
	}
}
