package wire

import (
	"testing"

	fuzz "github.com/google/gofuzz"
	"github.com/stretchr/testify/require"

	"github.com/spacemeshos/go-spacemesh/codec"
)

func fuzzDecoding[T Proof](t *testing.T, data []byte, proof T) {
	fuzzer := fuzz.NewFromGoFuzz(data)
	fuzzer.Fuzz(proof)

	atxProof := &ATXProof{
		Version:   0x01,
		ProofType: proof.Type(),

		Proof: codec.MustEncode(proof),
	}

	encodedAtxProof := codec.MustEncode(atxProof)
	decodedAtxProof := &ATXProof{}
	codec.MustDecode(encodedAtxProof, decodedAtxProof)

	decodedProof, err := decodedAtxProof.Decode()
	require.NoError(t, err)

	require.Equal(t, proof, decodedProof.(T))
}

func FuzzATXProofDecodeDoubleMarry(f *testing.F) {
	f.Add([]byte{0x01, 0x02, 0x03, 0x04, 0x05, 0x06})
	f.Fuzz(func(t *testing.T, data []byte) {
		fuzzDecoding(t, data, &ProofDoubleMarry{})
	})
}

func FuzzATXProofDecodeDoubleMerge(f *testing.F) {
	f.Add([]byte{0x01, 0x02, 0x03, 0x04, 0x05, 0x06})
	f.Fuzz(func(t *testing.T, data []byte) {
		fuzzDecoding(t, data, &ProofDoubleMerge{})
	})
}

func FuzzATXProofDecodeInvalidPost(f *testing.F) {
	f.Add([]byte{0x01, 0x02, 0x03, 0x04, 0x05, 0x06})
	f.Fuzz(func(t *testing.T, data []byte) {
		fuzzDecoding(t, data, &ProofInvalidPost{})
	})
}

func FuzzATXProofDecodeInvalidPrevAtxV1(f *testing.F) {
	f.Add([]byte{0x01, 0x02, 0x03, 0x04, 0x05, 0x06})
	f.Fuzz(func(t *testing.T, data []byte) {
		fuzzDecoding(t, data, &ProofInvalidPrevAtxV1{})
	})
}

func FuzzATXProofDecodeInvalidPrevAtxV2(f *testing.F) {
	f.Add([]byte{0x01, 0x02, 0x03, 0x04, 0x05, 0x06})
	f.Fuzz(func(t *testing.T, data []byte) {
		fuzzDecoding(t, data, &ProofInvalidPrevAtxV2{})
	})
}

func TestDecode(t *testing.T) {
	t.Run("unknown proof type", func(t *testing.T) {
		atxProof := &ATXProof{
			Version:   0x01,
			ProofType: 0x42, // unknown proof type
		}

		_, err := atxProof.Decode()
		require.ErrorContains(t, err, "unknown ATX malfeasance proof type")
	})

	t.Run("atx proof fails decoding", func(t *testing.T) {
		atxProof := &ATXProof{
			Version:   0x01,
			ProofType: DoubleMarry,
			Proof:     []byte{}, // invalid proof
		}

		_, err := atxProof.Decode()
		require.ErrorContains(t, err, "decoding ATX malfeasance proof of type 0x11")
	})
}
