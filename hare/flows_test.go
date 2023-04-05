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

// Test - run multiple CPs simultaneously.
func Test_multipleCPs(t *testing.T) {
	logtest.SetupGlobal(t)

	r := require.New(t)
	totalCp := uint32(3)
	finalLyr := types.GetEffectiveGenesis().Add(totalCp)
	test := newHareWrapper(totalCp)
	totalNodes := 10
	// RoundDuration is not used because we override the newRoundClock
	// function, wakeupDelta controls whether a consensus process will skip a
	// layer, if the layer tick arrives after wakeup delta then the process
	// skips the layer.
	cfg := config.Config{N: totalNodes, WakeupDelta: time.Hour, RoundDuration: 0, ExpectedLeaders: totalNodes/2 + 1, LimitIterations: 1000, LimitConcurrent: 100, Hdist: 20}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	mesh, err := mocknet.FullMeshLinked(totalNodes)
	require.NoError(t, err)
	defer func() {
		err := mesh.Close()
		require.NoError(t, err)
	}()

	test.initialSets = make([]*Set, totalNodes)

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

	// setup roundClocks to progress a layer only when all nodes have received messages from all nodes.
	roundClocks := newSharedRoundClocks(totalNodes*totalNodes, 200*time.Millisecond)
	var pubsubs []*pubsub.PubSub
	outputs := make([]map[types.LayerID]LayerOutput, totalNodes)
	var outputsWaitGroup sync.WaitGroup
	for i := 0; i < totalNodes; i++ {
		host := mesh.Hosts()[i]
		ps, err := pubsub.New(ctx, logtest.New(t), host, pubsub.DefaultConfig())
		require.NoError(t, err)
		pubsubs = append(pubsubs, ps)
		// We wrap the pubsub system to notify round clocks whenever a message
		// is received.
		testPs := &testPublisherSubscriber{
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
		r.NoError(e)
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
			// We can't allow layers to progress more than 1 ahead of the latest layer
			// that all nodes have started, otherwise we can end up losing messages
			// when a node from a future layer sends messages to a node that has not
			// started that layer. E.g A node that has started layer 8 receives a
			// message for layer 10, its broker will reject it because the broker only
			// allows messages one layer ahead not two.
			<-roundClocks.roundClock(j.Sub(1)).AwaitEndOfRound(preRound)
			ahead--
		}
		test.clock.advanceLayer()
		ahead++
	}

	test.WaitForTimedTermination(t, time.Minute)
	// We close hare here so that the blockGenCh is closed which allows for the
	// outputsWaitGroup to complete.
	for _, h := range test.hare {
		h.Close()
	}
	outputsWaitGroup.Wait()
	for _, out := range outputs {
		for lid := types.GetEffectiveGenesis().Add(1); !lid.After(finalLyr); lid = lid.Add(1) {
			require.NotNil(t, out[lid])
			require.ElementsMatch(t, types.ToProposalIDs(pList[lid]), out[lid].Proposals)
		}
	}
}

// Test - run multiple CPs where one of them runs more than one iteration.
func Test_multipleCPsAndIterations(t *testing.T) {
	logtest.SetupGlobal(t)

	r := require.New(t)
	totalCp := uint32(4)
	finalLyr := types.GetEffectiveGenesis().Add(totalCp)
	test := newHareWrapper(totalCp)
	totalNodes := 10
	// RoundDuration is not used because we override the newRoundClock
	// function, wakeupDelta controls whether a consensus process will skip a
	// layer, if the layer tick arrives after wakeup delta then the process
	// skips the layer.
	cfg := config.Config{N: totalNodes, WakeupDelta: time.Hour, RoundDuration: 0, ExpectedLeaders: totalNodes/2 + 1, LimitIterations: 1000, LimitConcurrent: 100, Hdist: 20}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	mesh, err := mocknet.FullMeshLinked(totalNodes)
	require.NoError(t, err)
	defer func() {
		err := mesh.Close()
		require.NoError(t, err)
	}()

	test.initialSets = make([]*Set, totalNodes)

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

	// setup roundClocks to progress a layer only when all nodes have received messages from all nodes.
	roundClocks := newSharedRoundClocks(totalNodes*totalNodes, 200*time.Millisecond)
	var pubsubs []*pubsub.PubSub
	outputs := make([]map[types.LayerID]LayerOutput, totalNodes)
	var outputsWaitGroup sync.WaitGroup
	var stalledLayerCount int
	var notifyMutex sync.Mutex
	// lets us wait so that all nodes start unstalled rounds in sync
	var targetLayerWg sync.WaitGroup
	targetLayerWg.Add(totalNodes)
	stalledLayer := types.GetEffectiveGenesis().Add(1)
	maxStalledRound := 7
	for i := 0; i < totalNodes; i++ {
		host := mesh.Hosts()[i]
		ps, err := pubsub.New(ctx, logtest.New(t), host, pubsub.DefaultConfig())
		require.NoError(t, err)

		testPs := &testPublisherSubscriber{
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
					notifyMutex.Lock()
					defer notifyMutex.Unlock()
					m, err := MessageFromBuffer(message)
					if err != nil {
						panic(err)
					}

					// Incremente message count.
					roundClocks.clock(m.Layer).incMessages(int(m.Eligibility.Count))

					// The stalled layer blocks messages from being published,
					// so after the preRound we need a different mechanism to
					// progress the rounds, because we will not be receiving
					// messages and therefore we will not be making calls to
					// incMessages.
					if m.Layer == stalledLayer && m.Round == preRound {
						// Keep track of received preRound messages
						stalledLayerCount += int(m.Eligibility.Count)
						if stalledLayerCount == totalNodes*totalNodes {
							// Once all preRound messages have been received wait for the preRound to complete.
							<-roundClocks.clock(m.Layer).AwaitEndOfRound(preRound)
							roundClocks.clock(m.Layer).advanceToRound(uint32(maxStalledRound + 1))
						}
					}
					return res
				})
			},
		}
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
		r.NoError(e)
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
			// We can't allow layers to progress more than 1 ahead of the latest layer
			// that all nodes have started, otherwise we can end up losing messages
			// when a node from a future layer sends messages to a node that has not
			// started that layer. E.g A node that has started layer 8 receives a
			// message for layer 10, its broker will reject it because the broker only
			// allows messages one layer ahead not two.
			<-roundClocks.roundClock(j.Sub(1)).AwaitEndOfRound(preRound)
			ahead--
		}
		test.clock.advanceLayer()
		ahead++
	}

	// There are 5 rounds per layer and totalCPs layers and we double to allow
	// for the for good measure. Also one layer in this test will run 2
	// iterations so we increase the layer count by 1.
	test.WaitForTimedTermination(t, time.Minute)
	for _, h := range test.hare {
		h.Close()
	}
	outputsWaitGroup.Wait()
	for _, out := range outputs {
		for lid := types.GetEffectiveGenesis().Add(1); !lid.After(finalLyr); lid = lid.Add(1) {
			require.NotNil(t, out[lid])
			require.ElementsMatch(t, types.ToProposalIDs(pList[lid]), out[lid].Proposals)
		}
	}
}
