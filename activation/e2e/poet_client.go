package activation

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"sync"
	"time"

	"github.com/spacemeshos/merkle-tree"
	"github.com/spacemeshos/merkle-tree/cache"
	"github.com/spacemeshos/merkle-tree/cache/readwriters"
	"github.com/spacemeshos/poet/hash"
	"github.com/spacemeshos/poet/shared"

	"github.com/spacemeshos/go-spacemesh/activation"
	"github.com/spacemeshos/go-spacemesh/common/types"
)

type TestPoet struct {
	mu    sync.Mutex
	round int

	expectedMembers int
	registrations   chan []byte
}

func NewTestPoetClient(expectedMembers int) *TestPoet {
	return &TestPoet{
		expectedMembers: expectedMembers,
		registrations:   make(chan []byte, expectedMembers),
	}
}

func (p *TestPoet) Id() []byte {
	return []byte(p.Address())
}

func (p *TestPoet) Address() string {
	return "http://poet.test"
}

func (p *TestPoet) PowParams(ctx context.Context) (*activation.PoetPowParams, error) {
	return &activation.PoetPowParams{}, nil
}

// SAFE to be called concurrently.
func (p *TestPoet) Submit(
	_ context.Context,
	_ time.Time,
	_, challenge []byte,
	_ types.EdSignature,
	_ types.NodeID,
	_ activation.PoetAuth,
) (*types.PoetRound, error) {
	if len(challenge) != 32 {
		return nil, errors.New("invalid challenge length")
	}
	p.mu.Lock()
	round := p.round
	p.mu.Unlock()
	p.registrations <- challenge

	return &types.PoetRound{ID: strconv.Itoa(round), End: time.Now()}, nil
}

func (p *TestPoet) CertifierInfo(ctx context.Context) (*types.CertifierInfo, error) {
	return nil, errors.New("CertifierInfo: not supported")
}

func (p *TestPoet) Info(ctx context.Context) (*types.PoetInfo, error) {
	return nil, errors.New("Info: not supported")
}

// Build a proof.
//
// Waits for the expected number of registrations to be submitted
// before starting to build the proof.
//
// NOT safe to be called concurrently.
func (p *TestPoet) Proof(ctx context.Context, roundID string) (*types.PoetProofMessage, []types.Hash32, error) {
	currentRoundId := strconv.Itoa(p.round)
	if roundID != currentRoundId {
		return nil, nil, fmt.Errorf("test error, invalid round ID (%s != expected %s)", roundID, currentRoundId)
	}

	mtree, err := merkle.NewTreeBuilder().WithHashFunc(shared.HashMembershipTreeNode).Build()
	if err != nil {
		return nil, nil, err
	}

	var members []types.Hash32
	for i := 0; i < p.expectedMembers; i++ {
		member := <-p.registrations
		if err := mtree.AddLeaf(member[:]); err != nil {
			return nil, nil, err
		}
		members = append(members, types.Hash32(member))
	}
	challenge := mtree.Root()

	const leaves = uint64(shared.T)

	treeCache := cache.NewWriter(
		func(uint) bool { return true },
		func(uint) (cache.LayerReadWriter, error) { return &readwriters.SliceReadWriter{}, nil },
	)

	labelHashFunc := hash.GenLabelHashFunc(challenge)
	mekleHashFunc := hash.GenMerkleHashFunc(challenge)
	tree, err := merkle.NewTreeBuilder().WithHashFunc(mekleHashFunc).WithCacheWriter(treeCache).Build()
	if err != nil {
		return nil, nil, err
	}

	makeLabel := shared.MakeLabelFunc()
	for i := range leaves {
		parkedNodes := tree.GetParkedNodes(nil)
		err := tree.AddLeaf(makeLabel(labelHashFunc, i, parkedNodes))
		if err != nil {
			return nil, nil, err
		}
	}

	root := tree.Root()
	cacheReader, err := treeCache.GetReader()
	if err != nil {
		return nil, nil, err
	}
	provenLeafIndices := shared.FiatShamir(root, leaves, shared.T)
	_, provenLeaves, proofNodes, err := merkle.GenerateProof(provenLeafIndices, cacheReader)
	if err != nil {
		return nil, nil, err
	}

	proof := &types.PoetProofMessage{
		PoetProof: types.PoetProof{
			MerkleProof: shared.MerkleProof{
				Root:         root,
				ProvenLeaves: provenLeaves,
				ProofNodes:   proofNodes,
			},
			LeafCount: leaves,
		},
		RoundID:       roundID,
		PoetServiceID: p.Id(),
		Statement:     types.Hash32(challenge),
	}

	p.mu.Lock()
	p.round++
	p.mu.Unlock()
	return proof, members, nil
}
