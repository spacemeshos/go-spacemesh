package hare

import (
	"github.com/spacemeshos/go-spacemesh/common"
	"github.com/spacemeshos/go-spacemesh/hare/config"
	"github.com/spacemeshos/go-spacemesh/mesh"
	"github.com/spacemeshos/go-spacemesh/p2p/service"
	"github.com/stretchr/testify/require"
	"sort"
	"sync"
	"testing"
	"time"
)

type mockOutput struct {
	id  []byte
	set *Set
}

func (m mockOutput) Id() []byte {
	return m.id
}
func (m mockOutput) Set() *Set {
	return m.set
}

type mockConsensusProcess struct {
	Closer
	t    chan TerminationOutput
	id   uint32
	term chan struct{}
	set  *Set
}

func (mcp *mockConsensusProcess) Start() error {
	if mcp.term != nil {
		<-mcp.term
	}
	mcp.Close()
	mcp.t <- mockOutput{common.Uint32ToBytes(mcp.id), mcp.set}
	return nil
}

func (mcp *mockConsensusProcess) Id() uint32 {
	return mcp.id
}

func (mcp *mockConsensusProcess) createInbox(size uint32) chan Message {
	c := make(chan Message)
	go func() {
		for {
			<-c
			// don't really need to use messages just don't block
		}
	}()
	return c
}

func NewMockConsensusProcess(cfg config.Config, instanceId InstanceId, s *Set, oracle Rolacle, signing Signing, p2p NetworkService, outputChan chan TerminationOutput) *mockConsensusProcess {
	mcp := new(mockConsensusProcess)
	mcp.Closer = NewCloser()
	mcp.id = common.BytesToUint32(instanceId.Bytes())
	mcp.t = outputChan
	mcp.set = s
	return mcp
}

var _ Consensus = (*mockConsensusProcess)(nil)

func TestNew(t *testing.T) {
	sim := service.NewSimulator()
	n1 := sim.NewNode()

	layerTicker := make(chan mesh.LayerID)

	oracle := NewMockHashOracle(numOfClients)
	signing := NewMockSigning()

	om := new(orphanMock)

	h := New(cfg, n1, signing, om, oracle, layerTicker)

	if h == nil {
		t.Fatal()
	}
}

func TestHare_Start(t *testing.T) {
	sim := service.NewSimulator()
	n1 := sim.NewNode()

	layerTicker := make(chan mesh.LayerID)

	oracle := NewMockHashOracle(numOfClients)
	signing := NewMockSigning()

	om := new(orphanMock)

	h := New(cfg, n1, signing, om, oracle, layerTicker)

	h.broker.Start() // todo: fix that hack. this will cause h.Start to return err

	err := h.Start()
	require.Error(t, err)

	h2 := New(cfg, n1, signing, om, oracle, layerTicker)
	require.NoError(t, h2.Start())
}

func TestHare_GetResult(t *testing.T) {
	sim := service.NewSimulator()
	n1 := sim.NewNode()

	layerTicker := make(chan mesh.LayerID)

	oracle := NewMockHashOracle(numOfClients)
	signing := NewMockSigning()

	om := new(orphanMock)

	h := New(cfg, n1, signing, om, oracle, layerTicker)

	res, err := h.GetResult(mesh.LayerID(0))

	require.Error(t, err)
	require.Nil(t, res)

	mockid := common.Uint32ToBytes(uint32(0))
	set := NewSetFromValues(Value{NewBytes32([]byte{0})})

	h.collectOutput(mockOutput{mockid, set})

	res, err = h.GetResult(mesh.LayerID(0))

	require.NoError(t, err)

	require.True(t, uint32(res[0]) == common.BytesToUint32(set.values[0].Bytes()))

}

func TestHare_GetResult2(t *testing.T) {
	sim := service.NewSimulator()
	n1 := sim.NewNode()

	layerTicker := make(chan mesh.LayerID)

	oracle := NewMockHashOracle(numOfClients)
	signing := NewMockSigning()

	om := new(orphanMock)

	h := New(cfg, n1, signing, om, oracle, layerTicker)

	h.networkDelta = 0

	h.factory = func(cfg config.Config, instanceId InstanceId, s *Set, oracle Rolacle, signing Signing, p2p NetworkService, outputChan chan TerminationOutput) Consensus {
		return NewMockConsensusProcess(cfg, instanceId, s, oracle, signing, p2p, outputChan)
	}

	h.Start()

	for i := 0; i < h.bufferSize+1; i++ {
		h.beginLayer <- mesh.LayerID(i)
	}
	time.Sleep(100 * time.Millisecond)

	_, err := h.GetResult(mesh.LayerID(h.bufferSize))
	require.NoError(t, err)

	h.beginLayer <- mesh.LayerID(h.bufferSize + 1)

	time.Sleep(100 * time.Millisecond)

	_, err = h.GetResult(0)
	require.Equal(t, err, ErrTooOld)
}

func TestHare_collectOutput(t *testing.T) {
	sim := service.NewSimulator()
	n1 := sim.NewNode()

	layerTicker := make(chan mesh.LayerID)

	oracle := NewMockHashOracle(numOfClients)
	signing := NewMockSigning()

	om := new(orphanMock)

	h := New(cfg, n1, signing, om, oracle, layerTicker)

	mockid := uint32(0)
	set := NewSetFromValues(Value{NewBytes32([]byte{0})})

	h.collectOutput(mockOutput{common.Uint32ToBytes(mockid), set})
	output, ok := h.outputs[mesh.LayerID(mockid)]
	require.True(t, ok)
	require.Equal(t, output[0], mesh.BlockID(common.BytesToUint32(set.values[0].Bytes())))

	mockid = uint32(2)

	output, ok = h.outputs[mesh.LayerID(mockid)] // todo : replace with getresult if this is yields a race
	require.False(t, ok)
	require.Nil(t, output)

}

func TestHare_collectOutput2(t *testing.T) {
	sim := service.NewSimulator()
	n1 := sim.NewNode()

	layerTicker := make(chan mesh.LayerID)

	oracle := NewMockHashOracle(numOfClients)
	signing := NewMockSigning()

	om := new(orphanMock)

	h := New(cfg, n1, signing, om, oracle, layerTicker)
	h.bufferSize = 1
	h.lastLayer = 0
	mockid := uint32(0)
	set := NewSetFromValues(Value{NewBytes32([]byte{0})})

	h.collectOutput(mockOutput{common.Uint32ToBytes(mockid), set})
	output, ok := h.outputs[mesh.LayerID(mockid)]
	require.True(t, ok)
	require.Equal(t, output[0], mesh.BlockID(common.BytesToUint32(set.values[0].Bytes())))

	h.lastLayer = 3
	newmockid := uint32(1)
	err := h.collectOutput(mockOutput{common.Uint32ToBytes(newmockid), set})
	require.Equal(t, err, ErrTooLate)

	newmockid2 := uint32(3)
	err = h.collectOutput(mockOutput{common.Uint32ToBytes(newmockid2), set})
	require.NoError(t, err)

	_, ok = h.outputs[0]

	require.False(t, ok)
}

func TestHare_onTick(t *testing.T) {
	cfg := config.DefaultConfig()

	cfg.N = 2
	cfg.F = 1
	cfg.RoundDuration = time.Millisecond
	cfg.SetSize = 3

	sim := service.NewSimulator()
	n1 := sim.NewNode()

	layerTicker := make(chan mesh.LayerID)

	oracle := NewMockHashOracle(numOfClients)
	signing := NewMockSigning()

	blockset := []mesh.BlockID{mesh.BlockID(0), mesh.BlockID(1), mesh.BlockID(2)}
	om := new(orphanMock)
	om.f = func() []mesh.BlockID {
		return blockset
	}

	h := New(cfg, n1, signing, om, oracle, layerTicker)
	h.networkDelta = 0
	h.bufferSize = 1

	createdChan := make(chan struct{})

	var nmcp *mockConsensusProcess
	h.factory = func(cfg config.Config, instanceId InstanceId, s *Set, oracle Rolacle, signing Signing, p2p NetworkService, outputChan chan TerminationOutput) Consensus {
		nmcp = NewMockConsensusProcess(cfg, instanceId, s, oracle, signing, p2p, outputChan)
		createdChan <- struct{}{}
		return nmcp
	}
	h.Start()

	var wg sync.WaitGroup

	wg.Add(2)
	go func() {
		wg.Done()
		layerTicker <- mesh.LayerID(0)
		<-createdChan
		<-nmcp.CloseChannel()
		wg.Done()
	}()

	//collect output one more time
	wg.Wait()
	time.Sleep(100 * time.Millisecond)
	res2, err := h.GetResult(mesh.LayerID(0))
	require.NoError(t, err)

	SortBlockIDs(res2)
	SortBlockIDs(blockset)

	require.Equal(t, blockset, res2)

	wg.Add(2)
	go func() {
		wg.Done()
		layerTicker <- mesh.LayerID(1)
		h.Close()
		wg.Done()
	}()

	//collect output one more time
	wg.Wait()
	_, err = h.GetResult(mesh.LayerID(1))
	require.Error(t, err)

}

type BlockIDSlice []mesh.BlockID

func (p BlockIDSlice) Len() int           { return len(p) }
func (p BlockIDSlice) Less(i, j int) bool { return p[i] < p[j] }
func (p BlockIDSlice) Swap(i, j int)      { p[i], p[j] = p[j], p[i] }

// Sort is a convenience method.
func (p BlockIDSlice) Sort() { sort.Sort(p) }

func SortBlockIDs(slice []mesh.BlockID) {
	sort.Sort(BlockIDSlice(slice))
}
