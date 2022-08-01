package types

import "github.com/spacemeshos/go-spacemesh/common/types"

type BlockCertificateConfig struct {
    CommitteeSize int
}
type BlockCertificate struct {
    BlockID               types.BlockID
    LayerID               types.LayerID
    TerminationSignatures []BlockSignature
}
type BlockSignature struct {
    SignerNodeID         types.NodeID
    SignerRoleProof      []byte
    SignerCommitteeSeats uint16

    BlockIDSignature []byte
}
type BlockSignatureMsg struct {
    LayerID types.LayerID
    BlockID types.BlockID
    BlockSignature
}
