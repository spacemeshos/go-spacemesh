package types

const (
	EdSignatureSize  = 64
	VrfSignatureSize = 80
)

type EdSignature [EdSignatureSize]byte

type VrfSignature [VrfSignatureSize]byte
