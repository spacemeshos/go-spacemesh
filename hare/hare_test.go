package hare

import (
	"bytes"
	"context"
	"sort"
	"sync"
	"testing"
	"time"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/common/util"
	"github.com/spacemeshos/go-spacemesh/eligibility"
	"github.com/spacemeshos/go-spacemesh/hare/config"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/p2p"
	"github.com/spacemeshos/go-spacemesh/p2p/service"
	signing2 "github.com/spacemeshos/go-spacemesh/signing"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func validateBlocks([]types.BlockID) bool {
	return true
}

type mockReport struct {
	id       instanceID
	set      *Set
	c        bool
	coinflip bool
}

func (m mockReport) ID() instanceID {
	return m.id
}
func (m mockReport) Set() *Set {
	return m.set
}

func (m mockReport) Completed() bool {
	return m.c
}

func (m mockReport) Coinflip() bool {
	return m.coinflip
}

type mockConsensusProcess struct {
	util.Closer
	t    chan TerminationOutput
	id   instanceID
	term chan struct{}
	set  *Set
}

func (mcp *mockConsensusProcess) Start(ctx context.Context) error {
	if mcp.term != nil {
		<-mcp.term
	}
	mcp.Close()
	mcp.t <- mockReport{mcp.id, mcp.set, true, false}
	return nil
}

func (mcp *mockConsensusProcess) ID() instanceID {
	return mcp.id
}

func (mcp *mockConsensusProcess) SetInbox(chan *Msg) {
}

type mockIDProvider struct {
	err error
}

func (mip *mockIDProvider) GetIdentity(edID string) (types.NodeID, error) {
	return types.NodeID{Key: edID, VRFPublicKey: []byte{}}, mip.err
}

func newMockConsensusProcess(cfg config.Config, instanceID instanceID, s *Set, oracle Rolacle, signing Signer, p2p NetworkService, outputChan chan TerminationOutput) *mockConsensusProcess {
	mcp := new(mockConsensusProcess)
	mcp.Closer = util.NewCloser()
	mcp.id = instanceID
	mcp.t = outputChan
	mcp.set = s
	return mcp
}

func createHare(n1 p2p.Service, logger log.Log) *Hare {
	return New(cfg, n1, signing2.NewEdSigner(), types.NodeID{}, validateBlocks, (&mockSyncer{true}).IsSynced, new(orphanMock), eligibility.New(), 10, &mockIDProvider{}, NewMockStateQuerier(), make(chan types.LayerID), logger)
}

var _ Consensus = (*mockConsensusProcess)(nil)

func TestNew(t *testing.T) {
	sim := service.NewSimulator()
	n1 := sim.NewNode()

	h := createHare(n1, log.NewDefault(t.Name()))

	if h == nil {
		t.Fatal()
	}
}

func TestHare_Start(t *testing.T) {
	sim := service.NewSimulator()
	n1 := sim.NewNode()

	h := createHare(n1, log.NewDefault(t.Name()))

	_ = h.broker.Start(context.TODO()) // todo: fix that hack. this will cause h.Start to return err

	/*err := h.Start()
	require.Error(t, err)*/

	h2 := createHare(n1, log.NewDefault(t.Name()))
	require.NoError(t, h2.Start(context.TODO()))
}

func TestHare_GetResult(t *testing.T) {
	r := require.New(t)
	sim := service.NewSimulator()
	n1 := sim.NewNode()

	h := createHare(n1, log.NewDefault(t.Name()))

	res, err := h.GetResult(types.LayerID(0))
	r.Equal(errNoResult, err)
	r.Nil(res)

	mockid := instanceID(0)
	set := NewSetFromValues(value1)

	_ = h.collectOutput(context.TODO(), mockReport{mockid, set, true, false})

	res, err = h.GetResult(types.LayerID(0))
	r.NoError(err)
	r.Equal(res[0].Bytes(), value1.Bytes())
}

func TestHare_GetResult2(t *testing.T) {
	sim := service.NewSimulator()
	n1 := sim.NewNode()

	om := new(orphanMock)
	om.f = func() []types.BlockID {
		return []types.BlockID{value1}
	}

	h := createHare(n1, log.NewDefault(t.Name()))
	h.msh = om

	h.networkDelta = 0

	h.factory = func(cfg config.Config, instanceId instanceID, s *Set, oracle Rolacle, signing Signer, p2p NetworkService, outputChan chan TerminationOutput) Consensus {
		return newMockConsensusProcess(cfg, instanceId, s, oracle, signing, p2p, outputChan)
	}

	_ = h.Start(context.TODO())

	for i := 1; i <= h.bufferSize; i++ {
		h.beginLayer <- types.LayerID(i)
		time.Sleep(15 * time.Millisecond)
	}
	time.Sleep(100 * time.Millisecond)

	_, err := h.GetResult(types.LayerID(h.bufferSize))
	require.NoError(t, err)

	h.beginLayer <- types.LayerID(h.bufferSize + 1)

	time.Sleep(100 * time.Millisecond)

	_, err = h.GetResult(0)
	require.Equal(t, err, errTooOld)
}

func TestHare_collectOutputCheckValidation(t *testing.T) {
	sim := service.NewSimulator()
	n1 := sim.NewNode()

	h := createHare(n1, log.NewDefault(t.Name()))

	mockid := instanceID1
	set := NewSetFromValues(value1)

	// default validation is true
	_ = h.collectOutput(context.TODO(), mockReport{mockid, set, true, false})
	output, ok := h.outputs[types.LayerID(mockid)]
	require.True(t, ok)
	require.Equal(t, output[0], value1)

	h.validate = func(blocks []types.BlockID) bool {
		return false
	}
	err := h.collectOutput(context.TODO(), mockReport{mockid, set, true, false})
	require.NoError(t, err)
	_, ok = h.outputs[types.LayerID(mockid)]
	require.True(t, ok, "failure to validate should only log an error and succeed")
}

func TestHare_collectOutput(t *testing.T) {
	sim := service.NewSimulator()
	n1 := sim.NewNode()

	h := createHare(n1, log.NewDefault(t.Name()))

	mockid := instanceID1
	set := NewSetFromValues(value1)

	_ = h.collectOutput(context.TODO(), mockReport{mockid, set, true, false})
	output, ok := h.outputs[types.LayerID(mockid)]
	require.True(t, ok)
	require.Equal(t, output[0], value1)

	mockid = instanceID2

	output, ok = h.outputs[types.LayerID(mockid)] // todo: replace with getresult if this yields a race
	require.False(t, ok)
	require.Nil(t, output)
}

func TestHare_collectOutput2(t *testing.T) {
	sim := service.NewSimulator()
	n1 := sim.NewNode()

	h := createHare(n1, log.NewDefault(t.Name()))
	h.bufferSize = 1
	h.lastLayer = 0
	mockid := instanceID0
	set := NewSetFromValues(value1)

	_ = h.collectOutput(context.TODO(), mockReport{mockid, set, true, false})
	output, ok := h.outputs[types.LayerID(mockid)]
	require.True(t, ok)
	require.Equal(t, output[0], value1)

	h.lastLayer = 3
	newmockid := instanceID1
	err := h.collectOutput(context.TODO(), mockReport{newmockid, set, true, false})
	require.Equal(t, err, ErrTooLate)

	newmockid2 := instanceID2
	err = h.collectOutput(context.TODO(), mockReport{newmockid2, set, true, false})
	require.NoError(t, err)

	_, ok = h.outputs[0]

	require.False(t, ok)
}

func TestHare_OutputCollectionLoop(t *testing.T) {
	types.SetLayersPerEpoch(4)
	sim := service.NewSimulator()
	n1 := sim.NewNode()

	h := createHare(n1, log.NewDefault(t.Name()))
	_ = h.Start(context.TODO())
	mo := mockReport{8, NewEmptySet(0), true, false}
	_, _ = h.broker.Register(context.TODO(), mo.ID())
	time.Sleep(1 * time.Second)
	h.outputChan <- mo
	time.Sleep(1 * time.Second)
	assert.Nil(t, h.broker.outbox[mo.ID()])
}

func TestHare_onTick(t *testing.T) {
	cfg := config.DefaultConfig()
	types.SetLayersPerEpoch(4)

	cfg.N = 2
	cfg.F = 1
	cfg.RoundDuration = 1

	sim := service.NewSimulator()
	n1 := sim.NewNode()

	layerTicker := make(chan types.LayerID)

	oracle := newMockHashOracle(numOfClients)
	signing := signing2.NewEdSigner()

	blockset := []types.BlockID{value1, value2, value3}
	om := new(orphanMock)
	om.f = func() []types.BlockID {
		return blockset
	}

	h := New(cfg, n1, signing, types.NodeID{}, validateBlocks, (&mockSyncer{true}).IsSynced, om, oracle, 10, &mockIDProvider{}, NewMockStateQuerier(), layerTicker, log.NewDefault("Hare"))
	h.networkDelta = 0
	h.bufferSize = 1

	createdChan := make(chan struct{})

	var nmcp *mockConsensusProcess
	h.factory = func(cfg config.Config, instanceId instanceID, s *Set, oracle Rolacle, signing Signer, p2p NetworkService, outputChan chan TerminationOutput) Consensus {
		nmcp = newMockConsensusProcess(cfg, instanceId, s, oracle, signing, p2p, outputChan)
		createdChan <- struct{}{}
		return nmcp
	}
	_ = h.Start(context.TODO())

	var wg sync.WaitGroup

	wg.Add(2)
	go func() {
		wg.Done()
		layerTicker <- types.GetEffectiveGenesis() + 1
		<-createdChan
		<-nmcp.CloseChannel()
		wg.Done()
	}()

	// collect output one more time
	wg.Wait()
	time.Sleep(100 * time.Millisecond)
	res2, err := h.GetResult(types.GetEffectiveGenesis() + 1)
	require.NoError(t, err)

	SortBlockIDs(res2)
	SortBlockIDs(blockset)

	require.Equal(t, blockset, res2)

	wg.Add(2)
	go func() {
		wg.Done()
		layerTicker <- types.GetEffectiveGenesis() + 2
		h.Close()
		wg.Done()
	}()

	// collect output one more time
	wg.Wait()
	res, err := h.GetResult(types.GetEffectiveGenesis() + 2)
	require.Equal(t, errNoResult, err)
	require.Equal(t, []types.BlockID(nil), res)
}

type BlockIDSlice []types.BlockID

func (p BlockIDSlice) Len() int           { return len(p) }
func (p BlockIDSlice) Less(i, j int) bool { return bytes.Compare(p[i].Bytes(), p[j].Bytes()) == -1 }
func (p BlockIDSlice) Swap(i, j int)      { p[i], p[j] = p[j], p[i] }

// Sort is a convenience method.
func (p BlockIDSlice) Sort() { sort.Sort(p) }

func SortBlockIDs(slice []types.BlockID) {
	sort.Sort(BlockIDSlice(slice))
}

func TestHare_outputBuffer(t *testing.T) {
	sim := service.NewSimulator()
	n1 := sim.NewNode()

	h := createHare(n1, log.NewDefault(t.Name()))
	lasti := types.LayerID(0)

	for i := types.LayerID(0); i < types.LayerID(h.bufferSize); i++ {
		h.lastLayer = i
		mockid := instanceID(i)
		set := NewSetFromValues(value1)
		_ = h.collectOutput(context.TODO(), mockReport{mockid, set, true, false})
		_, ok := h.outputs[types.LayerID(mockid)]
		require.True(t, ok)
		require.Equal(t, int(i+1), len(h.outputs))
		lasti = i
	}

	require.Equal(t, h.bufferSize, len(h.outputs))

	// add another output
	mockid := instanceID(lasti + 1)
	set := NewSetFromValues(value1)
	_ = h.collectOutput(context.TODO(), mockReport{mockid, set, true, false})
	_, ok := h.outputs[types.LayerID(mockid)]
	require.True(t, ok)
	require.Equal(t, h.bufferSize, len(h.outputs))
}

func TestHare_IsTooLate(t *testing.T) {
	sim := service.NewSimulator()
	n1 := sim.NewNode()

	h := createHare(n1, log.NewDefault(t.Name()))

	for i := types.LayerID(0); i < types.LayerID(h.bufferSize*2); i++ {
		mockid := instanceID(i)
		set := NewSetFromValues(value1)
		h.lastLayer = i
		_ = h.collectOutput(context.TODO(), mockReport{mockid, set, true, false})
		_, ok := h.outputs[types.LayerID(mockid)]
		require.True(t, ok)
		exp := int(i + 1)
		if exp > h.bufferSize {
			exp = h.bufferSize
		}

		require.Equal(t, exp, len(h.outputs))
	}

	require.True(t, h.outOfBufferRange(instanceID(1)))
}

func TestHare_oldestInBuffer(t *testing.T) {
	sim := service.NewSimulator()
	n1 := sim.NewNode()

	h := createHare(n1, log.NewDefault(t.Name()))
	lasti := types.LayerID(0)

	for i := types.LayerID(0); i < types.LayerID(h.bufferSize); i++ {
		mockid := instanceID(i)
		set := NewSetFromValues(value1)
		h.lastLayer = i
		_ = h.collectOutput(context.TODO(), mockReport{mockid, set, true, false})
		_, ok := h.outputs[types.LayerID(mockid)]
		require.True(t, ok)
		exp := int(i + 1)
		if exp > h.bufferSize {
			exp = h.bufferSize
		}

		require.Equal(t, exp, len(h.outputs))
		lasti = i
	}

	lyr := h.oldestResultInBuffer()
	require.True(t, lyr == 0)

	mockid := instanceID(lasti + 1)
	set := NewSetFromValues(value1)
	h.lastLayer = lasti + 1
	_ = h.collectOutput(context.TODO(), mockReport{mockid, set, true, false})
	_, ok := h.outputs[types.LayerID(mockid)]
	require.True(t, ok)
	require.Equal(t, h.bufferSize, len(h.outputs))

	lyr = h.oldestResultInBuffer()
	require.True(t, lyr == 1)

	mockid = instanceID(lasti + 2)
	set = NewSetFromValues(value1)
	h.lastLayer = lasti + 2
	_ = h.collectOutput(context.TODO(), mockReport{mockid, set, true, false})
	_, ok = h.outputs[types.LayerID(mockid)]
	require.True(t, ok)
	require.Equal(t, h.bufferSize, len(h.outputs))

	lyr = h.oldestResultInBuffer()
	require.Equal(t, types.LayerID(2), lyr)
}

// make sure that Hare writes a weak coin value for a layer to the mesh after the CP completes,
// regardless of whether it succeeds or fails
func TestHare_WeakCoin(t *testing.T) {
	r := require.New(t)
	sim := service.NewSimulator()
	n1 := sim.NewNode()

	layerID := types.LayerID(10)

	done := make(chan struct{})
	layerTicker := make(chan types.LayerID)
	oracle := newMockHashOracle(numOfClients)
	signing := signing2.NewEdSigner()
	om := &orphanMock{recordCoinflipsFn: func(_ context.Context, id types.LayerID, b bool) {
		r.Equal(layerID, id)
		r.True(b)
		done <- struct{}{}
	}}
	h := New(cfg, n1, signing, types.NodeID{}, validateBlocks, (&mockSyncer{true}).IsSynced, om, oracle, 10, &mockIDProvider{}, NewMockStateQuerier(), layerTicker, log.NewDefault("Hare"))
	defer h.Close()
	h.lastLayer = layerID
	set := NewSetFromValues(value1)

	_ = h.Start(context.TODO())
	waitForMsg := func() {
		tmr := time.NewTimer(time.Second)
		select {
		case <-tmr.C:
			r.Fail("timed out waiting for message")
		case <-done:
		}
	}
	h.outputChan <- mockReport{instanceID(layerID), set, true, true}
	h.outputChan <- mockReport{instanceID(layerID), set, false, true}
	waitForMsg()
	waitForMsg()
	om.recordCoinflipsFn = func(_ context.Context, id types.LayerID, b bool) {
		r.Equal(layerID, id)
		r.False(b)
		done <- struct{}{}
	}
	h.outputChan <- mockReport{instanceID(layerID), set, true, false}
	h.outputChan <- mockReport{instanceID(layerID), set, false, false}
	waitForMsg()
	waitForMsg()
	om.recordCoinflipsFn = func(_ context.Context, id types.LayerID, b bool) {
		r.Equal(layerID+1, id)
		r.True(b)
		done <- struct{}{}
	}
	h.outputChan <- mockReport{instanceID(layerID + 1), set, true, true}
	waitForMsg()
}
