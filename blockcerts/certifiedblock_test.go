package blockcerts_test

import (
    "context"
    "fmt"
    "github.com/golang/mock/gomock"
    "github.com/libp2p/go-libp2p-core/peer"
    "github.com/spacemeshos/go-spacemesh/blockcerts"
    "github.com/spacemeshos/go-spacemesh/blockcerts/config"
    certtypes "github.com/spacemeshos/go-spacemesh/blockcerts/types"
    "github.com/spacemeshos/go-spacemesh/codec"
    "github.com/spacemeshos/go-spacemesh/common/types"
    "github.com/spacemeshos/go-spacemesh/hare"
    haremocks "github.com/spacemeshos/go-spacemesh/hare/mocks"
    "github.com/spacemeshos/go-spacemesh/log/logtest"
    "github.com/spacemeshos/go-spacemesh/p2p/pubsub"
    pubsubmocks "github.com/spacemeshos/go-spacemesh/p2p/pubsub/mocks"
    signingmocks "github.com/spacemeshos/go-spacemesh/signing/mocks"
    "github.com/spacemeshos/go-spacemesh/sql/certifiedblocks"
    "go.uber.org/zap/zapcore"
    "time"

    "github.com/spacemeshos/go-spacemesh/sql"
    "github.com/stretchr/testify/require"
    "sync"
    "testing"
)

type certServiceTestRig struct {
    SUT                *blockcerts.BlockCertifyingService // System Under Test
    signer             *signingmocks.MockSigner
    rolacle            *haremocks.MockRolacle
    publisher          *pubsubmocks.MockPublisher
    hareTerminationsCh chan hare.TerminationBlockOutput
    db                 *sql.Database
}

func newBlockCertifyingServiceTestRig(t *testing.T,
    controller *gomock.Controller, config config.BlockCertificateConfig) (testRig *certServiceTestRig, err error) {

    testRig = &certServiceTestRig{}
    testRig.signer = signingmocks.NewMockSigner(controller)
    testRig.rolacle = haremocks.NewMockRolacle(controller)
    testRig.publisher = pubsubmocks.NewMockPublisher(controller)
    testRig.hareTerminationsCh = make(chan hare.TerminationBlockOutput)
    testRig.db = sql.InMemory()
    logger := logtest.New(t, zapcore.DebugLevel)

    testRig.SUT, err = blockcerts.NewBlockCertifyingService(
        testRig.hareTerminationsCh,
        testRig.rolacle,
        testRig.publisher,
        testRig.signer,
        testRig.db, config, logger)

    return testRig, nil
}

// TO TEST:
// Signing of blocks on hare termination
// Systest for block certifying service
// Received (from gossip) signatures are stored in cache
// Cache doesn't store signatures from before hdist (or whatever is set as threshold)
// BlockIDs with f+1 signatures are committed to database
// Certificates older than hdist (or parameter in config) are removed from database
// Cache layer boundary increases with hdist

func Test_ProducesExpectedBlockSignature_WhenChosenByOracle(t *testing.T) {
    mockCtrler := gomock.NewController(t)
    config := config.BlockCertificateConfig{
        CommitteeSize:  10,
        MaxAdversaries: 5,
        Hdist:          10,
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
    setExpectations_ProducesExpectedBlockSignature_WhenChosenByOracle(t,
        testRig, roundTerminationInfo, signalTestComplete)

    ctx, stopCertService := context.WithCancel(context.Background())
    err = testRig.SUT.Start(ctx)
    require.NoError(t, err)

    testRig.hareTerminationsCh <- roundTerminationInfo
    <-waitForPublishCh
    stopCertService()
}

func setExpectations_ProducesExpectedBlockSignature_WhenChosenByOracle(
    t *testing.T, rig *certServiceTestRig,
    roundTerminationInfo hare.TerminationBlockOutput, testComplete func(),
) {
    bloodsucking := []byte{42}
    rig.signer.EXPECT().Sign(gomock.Any()).Return(bloodsucking)
    var signerCommitteeSeats uint16 = 11
    rig.rolacle.EXPECT().CalcEligibility(gomock.Any(), gomock.Any(),
        gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(),
    ).Return(signerCommitteeSeats, nil)
    rig.rolacle.EXPECT().IsIdentityActiveOnConsensusView(gomock.Any(),
        gomock.Any(), gomock.Any(),
    ).Return(true, nil)
    signerRoleProof := []byte{24}
    rig.rolacle.EXPECT().Proof(gomock.Any(), gomock.Any(), gomock.Any(),
    ).Return(signerRoleProof, nil)
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

func Test_WaitingForCertificate_SignaturesFromGossip(t *testing.T) {
    mockController := gomock.NewController(t)
    conf := config.BlockCertificateConfig{CommitteeSize: 10, MaxAdversaries: 5, Hdist: 10}
    testRig, err := newBlockCertifyingServiceTestRig(t, mockController, conf)
    setExpectations_WaitingForCertificate_SignaturesFromGossip(testRig, conf.CommitteeSize)
    require.NoError(t, err)
    ctx := context.Background()
    gossipHandler := testRig.SUT.GossipHandler()
    gossiperWG := &sync.WaitGroup{}
    gossipSignaturesFromUniqueNodesTo(ctx, gossipHandler, conf.CommitteeSize,
        types.NewLayerID(81), types.EmptyBlockID, gossiperWG, t)

    err = testRig.SUT.Start(context.Background())
    require.NoError(t, err)
    gossiperWG.Wait()
    certInDB := func() bool {
        certExists, err := certifiedblocks.Has(testRig.db, types.EmptyBlockID)
        require.NoError(t, err)
        return certExists
    }
    require.Eventually(t, certInDB, 5*time.Second, time.Millisecond*100)
}
func gossipSignaturesFromUniqueNodesTo(ctx context.Context,
    gossipHandler pubsub.GossipHandler, committeeThreshold int,
    layerID types.LayerID, blockID types.BlockID, wg *sync.WaitGroup, t *testing.T) {

    for i := 0; i < committeeThreshold; i++ {
        nodeID := [32]byte{byte(i)}
        msg := certtypes.BlockSignatureMsg{
            LayerID: layerID,
            BlockID: blockID,
            BlockSignature: certtypes.BlockSignature{
                SignerNodeID:         nodeID,
                SignerRoleProof:      []byte{},
                SignerCommitteeSeats: 1,
                BlockIDSignature:     []byte{},
            },
        }
        peerID := peer.ID(fmt.Sprintf("peerID_%d", i))
        peerNumber := i
        msgBytes, err := codec.Encode(msg)
        require.NoError(t, err)
        wg.Add(1)
        go func() {
            t.Logf("%d started", peerNumber)
            gossipHandler(ctx, peerID, msgBytes)
            t.Logf("%d done", peerNumber)
            wg.Done()
        }()
    }
}
func setExpectations_WaitingForCertificate_SignaturesFromGossip(
    rig *certServiceTestRig, numSiganturesToValidate int) {
    // gossip comes in, handler validates role
    rig.rolacle.EXPECT().Validate(gomock.Any(), gomock.Any(), gomock.Any(),
        gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(),
    ).Return(true, nil).Times(numSiganturesToValidate)
    // signature is cached
    // once enough signatures are cached
    // a cert appears in the database
}

func Test_ReplayedSignaturesFromSameNode_DontCountTowardsThreshold(t *testing.T) {

}

func Test_WaitingForCertificate_SignaturesAddedToCache(t *testing.T) {

}

func Test_CertifiedBlockStore_StoresValidBlocks(t *testing.T) {

}
func Test_CertifiedBlockProvider_ProvidesValidBlocks(t *testing.T) {

}
