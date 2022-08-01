package blockcerts_test

import (
    "context"
    "github.com/golang/mock/gomock"
    "github.com/spacemeshos/go-spacemesh/blockcerts"
    certtypes "github.com/spacemeshos/go-spacemesh/blockcerts/types"
    "github.com/spacemeshos/go-spacemesh/codec"
    "github.com/spacemeshos/go-spacemesh/common/types"
    "github.com/spacemeshos/go-spacemesh/hare"
    haremocks "github.com/spacemeshos/go-spacemesh/hare/mocks"
    "github.com/spacemeshos/go-spacemesh/log/logtest"
    pubsubmocks "github.com/spacemeshos/go-spacemesh/p2p/pubsub/mocks"
    signingmocks "github.com/spacemeshos/go-spacemesh/signing/mocks"
    "github.com/spacemeshos/go-spacemesh/sql"
    "github.com/stretchr/testify/require"
    "testing"
)

type certServiceTestRig struct {
    SUT                *blockcerts.BlockCertifyingService // System Under Test
    signer             *signingmocks.MockSigner
    rolacle            *haremocks.MockRolacle
    publisher          *pubsubmocks.MockPublisher
    hareTerminationsCh chan hare.TerminationBlockOutput
}

func newBlockCertifyingServiceTestRig(t *testing.T,
    controller *gomock.Controller, config certtypes.BlockCertificateConfig) (testRig *certServiceTestRig, err error) {

    testRig = &certServiceTestRig{}
    testRig.signer = signingmocks.NewMockSigner(controller)
    testRig.rolacle = haremocks.NewMockRolacle(controller)
    testRig.publisher = pubsubmocks.NewMockPublisher(controller)
    testRig.hareTerminationsCh = make(chan hare.TerminationBlockOutput)
    logger := logtest.New(t)

    testRig.SUT, err = blockcerts.NewBlockCertifyingService(
        testRig.hareTerminationsCh,
        testRig.rolacle,
        testRig.publisher,
        testRig.signer,
        sql.InMemory(), config, logger)

    return testRig, nil
}

func expectations_ProducesValidBlockSignature_WhenChosenByOracle(
    t *testing.T, rig *certServiceTestRig,
    roundTerminationInfo hare.TerminationBlockOutput, testComplete func(),
) {
    bloodsucking := []byte{42}
    rig.signer.EXPECT().Sign(gomock.Any()).Return(bloodsucking)
    var signerCommitteeSeats uint16 = 11
    rig.rolacle.EXPECT().CalcEligibility(gomock.Any(), gomock.Any(),
        gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).
        Return(signerCommitteeSeats, nil)
    rig.rolacle.EXPECT().IsIdentityActiveOnConsensusView(gomock.Any(),
        gomock.Any(), gomock.Any()).Return(true, nil)
    signerRoleProof := []byte{24}
    rig.rolacle.EXPECT().Proof(gomock.Any(), gomock.Any(), gomock.Any()).
        Return(signerRoleProof, nil)
    // once a round of hare terminates, expect publish with signature of block
    rig.publisher.EXPECT().Publish(
        gomock.Any(),
        gomock.Eq(blockcerts.BlockSigTopic),
        gomock.AssignableToTypeOf([]byte{})).
        Do(func(_ context.Context, _ string, msgBytes []byte) {
            var msg certtypes.BlockSignatureMsg
            err := codec.Decode(msgBytes, &msg)
            require.NoError(t, err)
            require.Equal(t, roundTerminationInfo.LayerID, msg.LayerID)
            require.Equal(t, roundTerminationInfo.BlockID, msg.BlockID)
            require.Equal(t, roundTerminationInfo.TerminatingNodeID, msg.SignerNodeID)
            require.Equal(t, signerCommitteeSeats, msg.SignerCommitteeSeats)
            require.Equal(t, signerRoleProof, msg.SignerRoleProof)
            require.Equal(t, bloodsucking, msg.BlockIDSignature)
            testComplete()
        })
}

func Test_ProducesValidBlockSignature_WhenChosenByOracle(t *testing.T) {
    mockCtrler := gomock.NewController(t)
    config := certtypes.BlockCertificateConfig{
        CommitteeSize: 800,
    }
    roundTerminationInfo := hare.TerminationBlockOutput{
        LayerID:           types.NewLayerID(64),
        BlockID:           types.EmptyBlockID,
        TerminatingNodeID: types.NodeID{},
    }
    testRig, err := newBlockCertifyingServiceTestRig(t, mockCtrler, config)
    require.NoError(t, err)
    waitForPublishCh := make(chan struct{})
    signalTestComplete := func() { close(waitForPublishCh) }
    expectations_ProducesValidBlockSignature_WhenChosenByOracle(t,
        testRig, roundTerminationInfo, signalTestComplete)

    ctx, stopCertService := context.WithCancel(context.Background())
    err = testRig.SUT.Start(ctx)
    require.NoError(t, err)

    testRig.hareTerminationsCh <- roundTerminationInfo
    <-waitForPublishCh
    stopCertService()
}

func expectations_WaitingForCertificate_SignaturesFromGossip(t *testing.T,
    rig *certServiceTestRig, roundTerminationInfo hare.TerminationBlockOutput,
    testComplete func()) {

}

func Test_WaitingForCertificate_SignaturesFromGossip(t *testing.T) {
    mockController := gomock.NewController(t)
    config := certtypes.BlockCertificateConfig{CommitteeSize: 3}
    testRig, err := newBlockCertifyingServiceTestRig(t, mockController, config)
    require.NoError(t, err)

    ctx := context.Background()
    gossipHandler := testRig.SUT.GossipHandler()

    gossipHandler(ctx, "blah", []byte{})

}

func Test_WaitingForCertificate_SignaturesAddedToCache(t *testing.T) {

}

func Test_CertifiedBlockStore_StoresValidBlocks(t *testing.T) {

}
func Test_CertifiedBlockProvider_ProvidesValidBlocks(t *testing.T) {

}
