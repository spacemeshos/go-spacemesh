package hare

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/golang/mock/gomock"
	"github.com/libp2p/go-libp2p/core/peer"
	mocknet "github.com/libp2p/go-libp2p/p2p/net/mock"
	"github.com/stretchr/testify/require"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/hare/config"
	"github.com/spacemeshos/go-spacemesh/hare/mocks"
	"github.com/spacemeshos/go-spacemesh/log/logtest"
	"github.com/spacemeshos/go-spacemesh/p2p/pubsub"
)

// Test running multiple consensus processes in parallel. This can happen when
// a layer starts before the last layer has converged.
func Test_multipleCPs(t *testing.T) {
	logtest.SetupGlobal(t)
	totalNodes := 10

	// setup roundClocks to progress a layer only when all nodes have received messages from all nodes.
	roundClocks := newSharedRoundClocks(totalNodes*totalNodes, 200*time.Millisecond)
	wrapPubSub := func(ps pubsub.PublishSubsciber) pubsub.PublishSubsciber {
		return &testPublisherSubscriber{
			inner: ps,
			register: func(protocol string, handler pubsub.GossipHandler) {
				ps.Register(protocol, func(ctx context.Context, peer peer.ID, message []byte) pubsub.ValidationResult {
					res := handler(ctx, peer, message)
					// Update the round clock
					layer, eligibilityCount := extractInstanceID(message)
					roundClocks.clock(layer).incMessages(int(eligibilityCount))
					return res
				})
			},
		}
	}
	totalCp := uint32(3)
	runTest(t, totalNodes, totalCp, roundClocks, wrapPubSub)
}

// Test running multiple consensus processes in parallel. This can happen when
// a layer starts before the last layer has converged. One of the layers in
// this test is manipulated into running more than one iteration.
func Test_multipleCPsAndIterations(t *testing.T) {
	logtest.SetupGlobal(t)
	totalNodes := 10

	// setup roundClocks to progress a layer only when all nodes have received messages from all nodes.
	roundClocks := newSharedRoundClocks(totalNodes*totalNodes, 200*time.Millisecond)

	var stalledLayerMessageCount int
	var stalledLayerMu sync.Mutex
	// lets us wait so that all nodes start unstalled rounds in sync
	var targetLayerWg sync.WaitGroup
	targetLayerWg.Add(totalNodes)
	stalledLayer := types.GetEffectiveGenesis().Add(1)
	maxStalledRound := 7
	wrapPubSub := func(ps pubsub.PublishSubsciber) pubsub.PublishSubsciber {
		return &testPublisherSubscriber{
			inner: ps,
			publish: func(ctx context.Context, topic string, message []byte) error {
				// drop messages from rounds 0 - 7 for stalled layer but not for preRound.
				msg, _ := MessageFromBuffer(message)
				if msg.Layer == stalledLayer && int(msg.Round) <= maxStalledRound && msg.Round != preRound {
					return errors.New("fake error")
				}
				// We need to wait before publishing for the unstalled rounds
				// to make sure all nodes have reached the unstalled round
				// otherwise nodes that are still working through the stalled
				// rounds may interpret the message as contextually invalid.
				if msg.Layer == stalledLayer && int(msg.Round) == maxStalledRound+1 {
					targetLayerWg.Done()
					targetLayerWg.Wait()
				}
				return ps.Publish(ctx, topic, message)
			},
			register: func(topic string, handler pubsub.GossipHandler) {
				// Register a wrapped handler with the real pubsub
				ps.Register(topic, func(ctx context.Context, peer peer.ID, message []byte) pubsub.ValidationResult {
					// Call the handler
					res := handler(ctx, peer, message)

					// Now we will ensure the round clock is progressed properly.
					stalledLayerMu.Lock()
					defer stalledLayerMu.Unlock()
					m, err := MessageFromBuffer(message)
					if err != nil {
						panic(err)
					}

					// Increment message count.
					roundClocks.clock(m.Layer).incMessages(int(m.Eligibility.Count))

					// The stalled layer blocks messages from being published,
					// so after the preRound we need a different mechanism to
					// progress the rounds, because we will not be receiving
					// messages and therefore we will not be making calls to
					// incMessages.
					if m.Layer == stalledLayer && m.Round == preRound {
						// Keep track of received preRound messages
						stalledLayerMessageCount += int(m.Eligibility.Count)
						if stalledLayerMessageCount == totalNodes*totalNodes {
							// Once all preRound messages have been received wait for the preRound to complete.
							<-roundClocks.clock(m.Layer).AwaitEndOfRound(preRound)
							roundClocks.clock(m.Layer).advanceToRound(uint32(maxStalledRound + 1))
						}
					}
					return res
				})
			},
		}
	}
	totalCp := uint32(4)
	runTest(t, totalNodes, totalCp, roundClocks, wrapPubSub)
}

func runTest(t *testing.T, totalNodes int, totalCp uint32, roundClocks *sharedRoundClocks, wrapPubSub func(pubsub.PublishSubsciber) pubsub.PublishSubsciber) {
	finalLyr := types.GetEffectiveGenesis().Add(totalCp)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	mesh, err := mocknet.FullMeshLinked(totalNodes)
	require.NoError(t, err)

	test := newHareWrapper(totalCp)
	test.initialSets = make([]*Set, totalNodes)
	test.mesh = mesh

	pList := make(map[types.LayerID][]*types.Proposal)
	for j := types.GetEffectiveGenesis().Add(1); !j.After(finalLyr); j = j.Add(1) {
		for i := uint64(0); i < 20; i++ {
			p := genLayerProposal(j, []types.TransactionID{})
			p.EpochData = &types.EpochData{
				Beacon: types.EmptyBeacon,
			}
			pList[j] = append(pList[j], p)
		}
	}

	meshes := make([]*mocks.Mockmesh, 0, totalNodes)
	ctrl, ctx := gomock.WithContext(ctx, t)
	for i := 0; i < totalNodes; i++ {
		mockMesh := mocks.NewMockmesh(ctrl)
		mockMesh.EXPECT().GetEpochAtx(gomock.Any(), gomock.Any()).Return(&types.ActivationTxHeader{BaseTickHeight: 11, TickCount: 1}, nil).AnyTimes()
		mockMesh.EXPECT().VRFNonce(gomock.Any(), gomock.Any()).Return(types.VRFPostIndex(0), nil).AnyTimes()
		mockMesh.EXPECT().GetMalfeasanceProof(gomock.Any()).AnyTimes()
		mockMesh.EXPECT().SetWeakCoin(gomock.Any(), gomock.Any()).AnyTimes()
		for lid := types.GetEffectiveGenesis().Add(1); !lid.After(finalLyr); lid = lid.Add(1) {
			mockMesh.EXPECT().Proposals(lid).Return(pList[lid], nil)
			for _, p := range pList[lid] {
				mockMesh.EXPECT().GetAtxHeader(p.AtxID).Return(&types.ActivationTxHeader{BaseTickHeight: 11, TickCount: 1}, nil).AnyTimes()
				mockMesh.EXPECT().Ballot(p.Ballot.ID()).Return(&p.Ballot, nil).AnyTimes()
			}
		}
		mockMesh.EXPECT().Proposals(gomock.Any()).Return([]*types.Proposal{}, nil).AnyTimes()
		meshes = append(meshes, mockMesh)
	}
	// RoundDuration is not used because we override the newRoundClock function
	// of Hare to provide our own clock which progresses rounds when all nodes
	// are ready. WakeupDelta does not affect round wakeup time since that is
	// hardcoded to be instant in the shared round clock but it does control
	// how late a consensus process can start a layer and not skip it, as such
	// we set it to a huge value to prevent nodes accidentally skipping layers.
	cfg := config.Config{N: totalNodes, WakeupDelta: time.Hour, RoundDuration: 0, ExpectedLeaders: totalNodes / 4, LimitIterations: 1000, LimitConcurrent: 100, Hdist: 20}

	var pubsubs []*pubsub.PubSub
	outputs := make([]map[types.LayerID]LayerOutput, totalNodes)
	var outputsWaitGroup sync.WaitGroup
	for i := 0; i < totalNodes; i++ {
		host := mesh.Hosts()[i]
		ps, err := pubsub.New(ctx, logtest.New(t), host, pubsub.DefaultConfig())
		require.NoError(t, err)

		testPs := wrapPubSub(ps)
		h := createTestHare(t, meshes[i], cfg, test.clock, testPs, t.Name())
		// override the round clocks method to use our shared round clocks
		h.newRoundClock = roundClocks.roundClock
		h.mockRoracle.EXPECT().IsIdentityActiveOnConsensusView(gomock.Any(), gomock.Any(), gomock.Any()).Return(true, nil).AnyTimes()
		h.mockRoracle.EXPECT().Proof(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return(types.EmptyVrfSignature, nil).AnyTimes()
		h.mockRoracle.EXPECT().CalcEligibility(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return(uint16(1), nil).AnyTimes()
		h.mockRoracle.EXPECT().Validate(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return(true, nil).AnyTimes()
		outputsWaitGroup.Add(1)
		go func(idx int) {
			defer outputsWaitGroup.Done()
			for {
				select {
				case <-ctx.Done():
					return
				case out, ok := <-h.blockGenCh:
					if !ok {
						return
					}
					if outputs[idx] == nil {
						outputs[idx] = make(map[types.LayerID]LayerOutput)
					}
					outputs[idx][out.Layer] = out
				}
			}
		}(i)
		test.hare = append(test.hare, h.Hare)
		e := h.Start(ctx)
		require.NoError(t, e)
	}
	require.NoError(t, mesh.ConnectAllButSelf())
	require.Eventually(t, func() bool {
		for _, ps := range pubsubs {
			if len(ps.ProtocolPeers(pubsub.HareProtocol)) != len(mesh.Hosts())-1 {
				return false
			}
		}
		return true
	}, 5*time.Second, 10*time.Millisecond)

	ahead := 0
	for j := types.GetEffectiveGenesis().Add(1); !j.After(finalLyr); j = j.Add(1) {
		if ahead > 1 {
			// Since the broker keeps early messages only for the subsequent
			// layer to the most recent layer we need to ensure that the
			// difference between the most recent layer at two nodes is no more
			// than one. Otherwise if nodes get too far ahead the trailing
			// nodes will discard their messages. We do this by waiting for the
			// end of the first round of the layer prior to the one to be
			// started, which because the shared round clock only progresses
			// rounds when all nodes have received messages from all nodes,
			// ensures that all nodes have at least started the prior layer.
			<-roundClocks.roundClock(j.Sub(1)).AwaitEndOfRound(preRound)
			ahead--
		}
		test.clock.advanceLayer()
		ahead++
	}

	test.WaitForTimedTermination(t, time.Minute)
	// We close hare instances here so that the blockGenCh is closed which
	// allows for the outputsWaitGroup to complete.
	test.Close()
	outputsWaitGroup.Wait()
	for _, out := range outputs {
		for lid := types.GetEffectiveGenesis().Add(1); !lid.After(finalLyr); lid = lid.Add(1) {
			require.NotNil(t, out[lid])
			require.ElementsMatch(t, types.ToProposalIDs(pList[lid]), out[lid].Proposals)
		}
	}
}
