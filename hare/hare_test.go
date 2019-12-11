package hare

import (
	"bytes"
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/eligibility"
	"github.com/spacemeshos/go-spacemesh/hare/config"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/p2p"
	"github.com/spacemeshos/go-spacemesh/p2p/service"
	signing2 "github.com/spacemeshos/go-spacemesh/signing"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"sort"
	"sync"
	"testing"
	"time"
)

func validateBlocks(blocks []types.BlockID) bool {
	return true
}

type mockReport struct {
	id  instanceId
	set *Set
	c   bool
}

func (m mockReport) Id() instanceId {
	return m.id
}
func (m mockReport) Set() *Set {
	return m.set
}

func (m mockReport) Completed() bool {
	return m.c
}

type mockConsensusProcess struct {
	Closer
	t    chan TerminationOutput
	id   instanceId
	term chan struct{}
	set  *Set
}

func (mcp *mockConsensusProcess) Start() error {
	if mcp.term != nil {
		<-mcp.term
	}
	mcp.Close()
	mcp.t <- mockReport{mcp.id, mcp.set, true}
	return nil
}

func (mcp *mockConsensusProcess) Id() instanceId {
	return mcp.id
}

func (mcp *mockConsensusProcess) SetInbox(chan *Msg) {
}

type mockIdProvider struct {
	err error
}

func (mip *mockIdProvider) GetIdentity(edId string) (types.NodeId, error) {
	return types.NodeId{Key: edId, VRFPublicKey: []byte{}}, mip.err
}

func NewMockConsensusProcess(cfg config.Config, instanceId instanceId, s *Set, oracle Rolacle, signing Signer, p2p NetworkService, outputChan chan TerminationOutput) *mockConsensusProcess {
	mcp := new(mockConsensusProcess)
	mcp.Closer = NewCloser()
	mcp.id = instanceId
	mcp.t = outputChan
	mcp.set = s
	return mcp
}

func createHare(n1 p2p.Service) *Hare {
	return New(cfg, n1, signing2.NewEdSigner(), types.NodeId{}, validateBlocks, (&mockSyncer{true}).IsSynced, new(orphanMock), eligibility.New(), 10, &mockIdProvider{}, NewMockStateQuerier(), make(chan types.LayerID), log.NewDefault("Hare"))
}

var _ Consensus = (*mockConsensusProcess)(nil)

func TestNew(t *testing.T) {
	sim := service.NewSimulator()
	n1 := sim.NewNode()

	h := createHare(n1)

	if h == nil {
		t.Fatal()
	}
}

func TestHare_Start(t *testing.T) {
	sim := service.NewSimulator()
	n1 := sim.NewNode()

	h := createHare(n1)

	h.broker.Start() // todo: fix that hack. this will cause h.Start to return err

	/*err := h.Start()
	require.Error(t, err)*/

	h2 := createHare(n1)
	require.NoError(t, h2.Start())
}

func TestHare_GetResult(t *testing.T) {
	r := require.New(t)
	sim := service.NewSimulator()
	n1 := sim.NewNode()

	h := createHare(n1)

	res, err := h.GetResult(types.LayerID(0))
	r.Equal(errNoResult, err)
	r.Nil(res)

	mockid := instanceId(0)
	set := NewSetFromValues(value1)

	h.collectOutput(mockReport{mockid, set, true})

	res, err = h.GetResult(types.LayerID(0))
	r.NoError(err)
	r.Equal(res[0].ToBytes(), value1.ToBytes())
}

func TestHare_GetResult2(t *testing.T) {
	sim := service.NewSimulator()
	n1 := sim.NewNode()

	om := new(orphanMock)
	om.f = func() []types.BlockID {
		return []types.BlockID{value1}
	}

	h := createHare(n1)
	h.obp = om

	h.networkDelta = 0

	h.factory = func(cfg config.Config, instanceId instanceId, s *Set, oracle Rolacle, signing Signer, p2p NetworkService, outputChan chan TerminationOutput) Consensus {
		return NewMockConsensusProcess(cfg, instanceId, s, oracle, signing, p2p, outputChan)
	}

	h.Start()

	for i := 1; i <= h.bufferSize; i++ {
		h.beginLayer <- types.LayerID(i)
		time.Sleep(10 * time.Millisecond)
	}
	time.Sleep(100 * time.Millisecond)

	_, err := h.GetResult(types.LayerID(h.bufferSize))
	require.NoError(t, err)

	h.beginLayer <- types.LayerID(h.bufferSize + 1)

	time.Sleep(100 * time.Millisecond)

	_, err = h.GetResult(0)
	require.Equal(t, err, ErrTooOld)
}

func TestHare_collectOutputCheckValidation(t *testing.T) {
	sim := service.NewSimulator()
	n1 := sim.NewNode()

	h := createHare(n1)

	mockid := instanceId1
	set := NewSetFromValues(value1)

	// default validation is true
	h.collectOutput(mockReport{mockid, set, true})
	output, ok := h.outputs[types.LayerID(mockid)]
	require.True(t, ok)
	require.Equal(t, output[0], value1)

	// make sure we panic for false
	defer func() {
		err := recover()
		require.Equal(t, err, "Failed to validate the collected output set")
	}()
	h.validate = func(blocks []types.BlockID) bool {
		return false
	}
	h.collectOutput(mockReport{mockid, set, true})
	_, ok = h.outputs[types.LayerID(mockid)]
	require.False(t, ok)
}

func TestHare_collectOutput(t *testing.T) {
	sim := service.NewSimulator()
	n1 := sim.NewNode()

	h := createHare(n1)

	mockid := instanceId1
	set := NewSetFromValues(value1)

	h.collectOutput(mockReport{mockid, set, true})
	output, ok := h.outputs[types.LayerID(mockid)]
	require.True(t, ok)
	require.Equal(t, output[0], value1)

	mockid = instanceId2

	output, ok = h.outputs[types.LayerID(mockid)] // todo : replace with getresult if this is yields a race
	require.False(t, ok)
	require.Nil(t, output)

}

func TestHare_collectOutput2(t *testing.T) {
	sim := service.NewSimulator()
	n1 := sim.NewNode()

	h := createHare(n1)
	h.bufferSize = 1
	h.lastLayer = 0
	mockid := instanceId0
	set := NewSetFromValues(value1)

	h.collectOutput(mockReport{mockid, set, true})
	output, ok := h.outputs[types.LayerID(mockid)]
	require.True(t, ok)
	require.Equal(t, output[0], value1)

	h.lastLayer = 3
	newmockid := instanceId1
	err := h.collectOutput(mockReport{newmockid, set, true})
	require.Equal(t, err, ErrTooLate)

	newmockid2 := instanceId2
	err = h.collectOutput(mockReport{newmockid2, set, true})
	require.NoError(t, err)

	_, ok = h.outputs[0]

	require.False(t, ok)
}

func TestHare_OutputCollectionLoop(t *testing.T) {
	sim := service.NewSimulator()
	n1 := sim.NewNode()

	h := createHare(n1)
	h.Start()
	mo := mockReport{1, NewEmptySet(0), true}
	h.broker.Register(mo.Id())
	time.Sleep(1 * time.Second)
	h.outputChan <- mo
	time.Sleep(1 * time.Second)
	assert.Nil(t, h.broker.outbox[mo.Id()])
}

func TestHare_onTick(t *testing.T) {
	cfg := config.DefaultConfig()

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

	h := New(cfg, n1, signing, types.NodeId{}, validateBlocks, (&mockSyncer{true}).IsSynced, om, oracle, 10, &mockIdProvider{}, NewMockStateQuerier(), layerTicker, log.NewDefault("Hare"))
	h.networkDelta = 0
	h.bufferSize = 1

	createdChan := make(chan struct{})

	var nmcp *mockConsensusProcess
	h.factory = func(cfg config.Config, instanceId instanceId, s *Set, oracle Rolacle, signing Signer, p2p NetworkService, outputChan chan TerminationOutput) Consensus {
		nmcp = NewMockConsensusProcess(cfg, instanceId, s, oracle, signing, p2p, outputChan)
		createdChan <- struct{}{}
		return nmcp
	}
	h.Start()

	var wg sync.WaitGroup

	wg.Add(2)
	go func() {
		wg.Done()
		layerTicker <- 1
		<-createdChan
		<-nmcp.CloseChannel()
		wg.Done()
	}()

	//collect output one more time
	wg.Wait()
	time.Sleep(100 * time.Millisecond)
	res2, err := h.GetResult(types.LayerID(1))
	require.NoError(t, err)

	SortBlockIDs(res2)
	SortBlockIDs(blockset)

	require.Equal(t, blockset, res2)

	wg.Add(2)
	go func() {
		wg.Done()
		layerTicker <- 2
		h.Close()
		wg.Done()
	}()

	//collect output one more time
	wg.Wait()
	res, err := h.GetResult(types.LayerID(2))
	require.Equal(t, errNoResult, err)
	require.Equal(t, []types.BlockID(nil), res)

}

type BlockIDSlice []types.BlockID

func (p BlockIDSlice) Len() int           { return len(p) }
func (p BlockIDSlice) Less(i, j int) bool { return bytes.Compare(p[i].ToBytes(), p[j].ToBytes()) == -1 }
func (p BlockIDSlice) Swap(i, j int)      { p[i], p[j] = p[j], p[i] }

// Sort is a convenience method.
func (p BlockIDSlice) Sort() { sort.Sort(p) }

func SortBlockIDs(slice []types.BlockID) {
	sort.Sort(BlockIDSlice(slice))
}

func TestHare_outputBuffer(t *testing.T) {
	sim := service.NewSimulator()
	n1 := sim.NewNode()

	h := createHare(n1)
	lasti := types.LayerID(0)

	for i := types.LayerID(0); i < types.LayerID(h.bufferSize); i++ {
		h.lastLayer = i
		mockid := instanceId(i)
		set := NewSetFromValues(value1)
		h.collectOutput(mockReport{mockid, set, true})
		_, ok := h.outputs[types.LayerID(mockid)]
		require.True(t, ok)
		require.Equal(t, int(i+1), len(h.outputs))
		lasti = i
	}

	require.Equal(t, h.bufferSize, len(h.outputs))

	// add another output
	mockid := instanceId(lasti + 1)
	set := NewSetFromValues(value1)
	h.collectOutput(mockReport{mockid, set, true})
	_, ok := h.outputs[types.LayerID(mockid)]
	require.True(t, ok)
	require.Equal(t, h.bufferSize, len(h.outputs))

}

func TestHare_IsTooLate(t *testing.T) {
	sim := service.NewSimulator()
	n1 := sim.NewNode()

	h := createHare(n1)

	for i := types.LayerID(0); i < types.LayerID(h.bufferSize*2); i++ {
		mockid := instanceId(i)
		set := NewSetFromValues(value1)
		h.lastLayer = i
		h.collectOutput(mockReport{mockid, set, true})
		_, ok := h.outputs[types.LayerID(mockid)]
		require.True(t, ok)
		exp := int(i + 1)
		if exp > h.bufferSize {
			exp = h.bufferSize
		}

		require.Equal(t, exp, len(h.outputs))
	}

	require.True(t, h.outOfBufferRange(instanceId(1)))
}

func TestHare_oldestInBuffer(t *testing.T) {
	sim := service.NewSimulator()
	n1 := sim.NewNode()

	h := createHare(n1)
	lasti := types.LayerID(0)

	for i := types.LayerID(0); i < types.LayerID(h.bufferSize); i++ {
		mockid := instanceId(i)
		set := NewSetFromValues(value1)
		h.lastLayer = i
		h.collectOutput(mockReport{mockid, set, true})
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

	mockid := instanceId(lasti + 1)
	set := NewSetFromValues(value1)
	h.lastLayer = lasti + 1
	h.collectOutput(mockReport{mockid, set, true})
	_, ok := h.outputs[types.LayerID(mockid)]
	require.True(t, ok)
	require.Equal(t, h.bufferSize, len(h.outputs))

	lyr = h.oldestResultInBuffer()
	require.True(t, lyr == 1)

	mockid = instanceId(lasti + 2)
	set = NewSetFromValues(value1)
	h.lastLayer = lasti + 2
	h.collectOutput(mockReport{mockid, set, true})
	_, ok = h.outputs[types.LayerID(mockid)]
	require.True(t, ok)
	require.Equal(t, h.bufferSize, len(h.outputs))

	lyr = h.oldestResultInBuffer()
	require.True(t, lyr == 2)

}
