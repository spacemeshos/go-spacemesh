package blockcerts

import (
    "context"
    "github.com/spacemeshos/go-spacemesh/blockcerts/types"
    "github.com/spacemeshos/go-spacemesh/codec"
    "github.com/spacemeshos/go-spacemesh/hare"
    "github.com/spacemeshos/go-spacemesh/log"
    "github.com/spacemeshos/go-spacemesh/p2p/pubsub"
    "github.com/spacemeshos/go-spacemesh/signing"
)

func blockSigningLoop(ctx context.Context,
    hareTerminationsCh <-chan hare.TerminationBlockOutput,               // input
    blockSigner signing.Signer, committeeSize int, rolacle hare.Rolacle, // dependencies
    gossipPublisher pubsub.Publisher, signatureCache *sigCache,          // output
    logger log.Logger,
) {
    logger = logger.WithContext(ctx)
    for {
        select {
        case <-ctx.Done():
            return

        case hareTermination, moreToCome := <-hareTerminationsCh:
            if !moreToCome {
                logger.Debug("block certification service: " +
                    "hare termination channel closed.")
                return
            }
            // 1. check readiness
            ready, err := rolacle.IsIdentityActiveOnConsensusView(ctx,
                hareTermination.TerminatingNodeID,
                hareTermination.LayerID,
            )
            if err != nil {
                logger.Error("error checking if active on consensus view")
            }
            if !ready {
                logger.Debug("not active on consensus view: " +
                    "not eligible to get committee seats.")
                break
            }
            // 2. calculate eligibility
            proof, err := rolacle.Proof(ctx,
                hareTermination.LayerID, blockCertifierRole)
            if err != nil {
                logger.Error("could not retrieve eligibility proof "+
                    "from oracle: %v", err)
                break
            }
            committeeSeatCount, err := rolacle.CalcEligibility(ctx,
                hareTermination.LayerID, blockCertifierRole, committeeSize,
                hareTermination.TerminatingNodeID, proof)

            blockIDBytes := hareTermination.BlockID.Bytes()
            blockIDSignature := blockSigner.Sign(blockIDBytes)

            blockSigMsg := types.BlockSignatureMsg{}
            blockSigMsg.LayerID = hareTermination.LayerID
            blockSigMsg.SignerNodeID = hareTermination.TerminatingNodeID
            blockSigMsg.SignerCommitteeSeats = committeeSeatCount
            blockSigMsg.SignerRoleProof = proof
            blockSigMsg.BlockID = hareTermination.BlockID
            blockSigMsg.BlockIDSignature = blockIDSignature

            msgBytes, err := codec.Encode(blockSigMsg)
            if err != nil {
                logger.Error("BlockCertifyService: failed to encode"+
                    "block signature: %v", err)
                break
            }
            err = gossipPublisher.Publish(ctx, BlockSigTopic, msgBytes)
            if err != nil {
                logger.Error("BlockCertifyService: failed to publish"+
                    "block signature: %v", err)
                break
            }

            signatureCache.CacheBlockSignature(ctx, blockSigMsg)
        }
    }
}
