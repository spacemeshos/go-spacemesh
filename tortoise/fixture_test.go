package tortoise

import (
	"context"
	"fmt"
	"math/rand/v2"
	"sort"
	"strconv"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/spacemeshos/go-spacemesh/atxsdata"
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/common/types/result"
	"github.com/spacemeshos/go-spacemesh/hash"
	"github.com/spacemeshos/go-spacemesh/log/logtest"
	"github.com/spacemeshos/go-spacemesh/tortoise/opinionhash"
)

type atxData struct {
	target types.EpochID
	id     types.ATXID
	atxsdata.ATX
}

type smesher struct {
	reg registry

	id       int
	defaults atxData
	atxs     map[types.ATXID]*atxAction
}

type atxOpt func(*atxData)

type aopt struct {
	opts []atxOpt
}

func (a *aopt) height(val uint64) *aopt {
	a.opts = append(a.opts, func(header *atxData) {
		header.Height = val
	})
	return a
}

func (a *aopt) weight(val uint64) *aopt {
	a.opts = append(a.opts, func(header *atxData) {
		header.Weight = val
	})
	return a
}

func (s *smesher) malfeasant() {
	smesher := hash.Sum([]byte(strconv.Itoa(s.id)))
	s.reg.register(&malfeasantAction{id: smesher})
}

func (s *smesher) rawatx(id types.ATXID, publish types.EpochID, opts ...*aopt) *atxAction {
	atx := s.defaults
	atx.id = id
	atx.target = publish + 1
	atx.Node = hash.Sum([]byte(strconv.Itoa(s.id)))
	for _, opt := range opts {
		for _, o := range opt.opts {
			o(&atx)
		}
	}
	val := &atxAction{
		reg:     s.reg,
		atx:     atx,
		ballots: map[types.BallotID]*ballotAction{},
	}
	s.reg.register(val)
	s.atxs[id] = val
	return val
}

// atx accepts publish epoch and atx fields that are relevant for tortoise
// as options.
func (s *smesher) atx(publish types.EpochID, opts ...*aopt) *atxAction {
	id := hash.Sum([]byte(strconv.Itoa(s.id)), []byte(strconv.Itoa(int(publish))))
	if val, exists := s.atxs[id]; exists {
		return val
	}
	return s.rawatx(id, publish, opts...)
}

type malfeasantAction struct {
	id types.NodeID
}

func (a *malfeasantAction) String() string {
	return fmt.Sprintf("malfeasant %v", a.id)
}

func (*malfeasantAction) deps() []action {
	return nil
}

func (a *malfeasantAction) execute(trt *Tortoise) {
	trt.OnMalfeasance(a.id)
}

type atxAction struct {
	reg registry

	atx       atxData
	ballots   map[types.BallotID]*ballotAction
	reference *ballotAction
}

func (a *atxAction) String() string {
	return fmt.Sprintf("atx %v", a.atx.id)
}

func (*atxAction) deps() []action {
	return nil
}

type ballotOpt func(*ballotAction)

type bopt struct {
	opts []ballotOpt
}

func (b *bopt) beacon(value string) *bopt {
	b.opts = append(b.opts, func(ballot *ballotAction) {
		if ballot.EpochData == nil {
			ballot.EpochData = &types.ReferenceData{}
		}
		copy(ballot.EpochData.Beacon[:], value)
	})
	return b
}

func (b *bopt) eligibilities(value int) *bopt {
	b.opts = append(b.opts, func(ballot *ballotAction) {
		ballot.Eligibilities = uint32(value)
	})
	return b
}

func (b *bopt) totalEligibilities(n int) *bopt {
	b.opts = append(b.opts, func(ballot *ballotAction) {
		if ballot.EpochData == nil {
			ballot.EpochData = &types.ReferenceData{}
		}
		ballot.EpochData.Eligibilities = uint32(n)
	})
	return b
}

// encoded votes.
type evotes struct {
	baseBallot *ballotAction
	votes      types.Votes
}

func (e *evotes) base(base *ballotAction) *evotes {
	e.votes.Base = base.ID
	e.baseBallot = base
	return e
}

func (e *evotes) abstain(lid uint32) *evotes {
	e.votes.Abstain = append(e.votes.Abstain, types.GetEffectiveGenesis()+types.LayerID(lid))
	return e
}

func (e *evotes) support(lid int, id string, height uint64) *evotes {
	vote := types.Vote{
		LayerID: types.GetEffectiveGenesis() + types.LayerID(lid),
		Height:  height,
	}
	copy(vote.ID[:], id)
	e.votes.Support = append(e.votes.Support, vote)
	return e
}

func (e *evotes) against(lid uint32, id string, height uint64) *evotes {
	vote := types.Vote{
		LayerID: types.GetEffectiveGenesis().Add(lid),
		Height:  height,
	}
	copy(vote.ID[:], id)
	e.votes.Against = append(e.votes.Against, vote)
	return e
}

func (b *bopt) votes(value *evotes) *bopt {
	b.opts = append(b.opts, func(ballot *ballotAction) {
		ballot.base = value.baseBallot
		ballot.Opinion.Votes = value.votes
	})
	return b
}

func (b *bopt) assert(onDecode func(*DecodedBallot, error), onStore func(error)) *bopt {
	b.opts = append(b.opts, func(ballot *ballotAction) {
		ballot.onDecoded = onDecode
		ballot.onStored = onStore
	})
	return b
}

func (a *atxAction) execute(trt *Tortoise) {
	trt.trtl.atxsdata.AddAtx(a.atx.target, a.atx.id, &a.atx.ATX)
	trt.OnAtx(a.atx.target, a.atx.id, &a.atx.ATX)
}

func (a *atxAction) rawballot(id types.BallotID, n int, opts ...*bopt) *ballotAction {
	lid := uint32(n) + types.GetEffectiveGenesis().Uint32()
	b := types.BallotTortoiseData{}
	b.Smesher = a.atx.Node
	b.AtxID = a.atx.id
	b.Layer = types.LayerID(lid)
	b.ID = id

	val := &ballotAction{
		BallotTortoiseData: b,
		onDecoded: func(db *DecodedBallot, err error) {
			if err != nil {
				panic(err)
			}
		},
		onStored: func(err error) {
			if err != nil {
				panic(err)
			}
		},
	}
	for _, opt := range opts {
		for _, o := range opt.opts {
			o(val)
		}
	}
	if a.reference == nil {
		a.reference = val
		val.before = append(val.before, a)
	} else {
		val.Ref = &a.reference.ID
		val.before = append(val.before, a.reference)
	}
	if (val.Opinion.Hash == types.Hash32{}) {
		val.Opinion.Hash = val.computeOpinion()
	}

	if val.base != nil {
		val.before = append(val.before, val.base)
	}

	a.reg.register(val)
	a.ballots[id] = val
	return val
}

func (a *atxAction) ballot(n int, opts ...*bopt) *ballotAction {
	hs := hash.Sum(a.atx.id[:], []byte(strconv.Itoa(int(n))))
	var id types.BallotID
	copy(id[:], hs[:])
	if val, exist := a.ballots[id]; exist {
		return val
	}
	return a.rawballot(id, n, opts...)
}

type ballotAction struct {
	types.BallotTortoiseData
	base   *ballotAction
	before []action

	onDecoded func(*DecodedBallot, error)
	onStored  func(error)
}

func (b *ballotAction) String() string {
	return fmt.Sprintf("ballot %v", b.ID)
}

func (b *ballotAction) deps() []action {
	return b.before
}

func (b *ballotAction) computeOpinion() types.Hash32 {
	votes := make([][]types.Vote, b.BallotTortoiseData.Layer.
		Difference(types.GetEffectiveGenesis()),
	) // vote on every layer before the ballot till effective genesis
	for i := range votes {
		votes[i] = []types.Vote{} // against by default
	}
	b.decodeVotes(votes)
	hasher := opinionhash.New()
	rst := types.Hash32{}
	for i, layer := range votes {
		if i != 0 {
			hasher.WritePrevious(rst)
		}
		if layer == nil {
			hasher.WriteAbstain()
		}
		if len(layer) > 0 {
			sort.Slice(layer, func(i, j int) bool {
				if layer[i].Height < layer[j].Height {
					return true
				}
				return layer[i].ID.Compare(layer[j].ID)
			})
		}
		for _, vote := range layer {
			hasher.WriteSupport(vote.ID, vote.Height)
		}
		hasher.Sum(rst[:0])
		hasher.Reset()
	}
	return rst
}

func (b *ballotAction) decodeVotes(votes [][]types.Vote) {
	if b.base != nil {
		b.base.decodeVotes(votes)
	}
	genesis := types.GetEffectiveGenesis()
	for _, vote := range b.Opinion.Support {
		pos := vote.LayerID.Difference(genesis)
		matches := false
		for _, existing := range votes[pos] {
			if existing == vote {
				matches = true
			}
		}
		if !matches {
			votes[pos] = append(votes[pos], vote)
		}
	}
	for _, vote := range b.Opinion.Against {
		pos := vote.LayerID.Difference(genesis)
		for i, existing := range votes[pos] {
			if existing == vote {
				votes[pos] = append(votes[pos][:i], votes[pos][i:]...)
				break
			}
		}
	}
	for _, vote := range b.Opinion.Abstain {
		pos := vote.Difference(genesis)
		votes[pos] = nil
	}
}

func (b *ballotAction) execute(trt *Tortoise) {
	decoded, err := trt.DecodeBallot(&b.BallotTortoiseData)
	b.onDecoded(decoded, err)
	err = trt.StoreBallot(decoded)
	b.onStored(err)
}

type action interface {
	execute(*Tortoise)
	deps() []action
	String() string
}

func newSession(tb testing.TB) *session {
	effective := types.GetEffectiveGenesis()
	epochSize := types.GetLayersPerEpoch()
	tb.Cleanup(func() {
		types.SetEffectiveGenesis(effective.Uint32())
		types.SetLayersPerEpoch(epochSize)
	})
	s := &session{tb: tb}
	return s.
		withLayerSize(50).
		withEpochSize(10).
		withHdist(10).
		withZdist(8).
		withDelay(10).
		withWindow(30)
}

type session struct {
	tb        testing.TB
	epochSize int
	layerSize int
	hdist     int
	config    *Config

	smeshers map[int]*smesher
	actions  []action
}

type registry interface {
	register(...action)
}

func (s *session) smesher(id int) *smesher {
	if s.smeshers == nil {
		s.smeshers = map[int]*smesher{}
	}
	if val, exist := s.smeshers[id]; exist {
		return val
	}
	val := &smesher{reg: s, id: id, atxs: map[types.ATXID]*atxAction{}}
	s.smeshers[id] = val
	return val
}

func (s *session) epochEligibilities() int {
	return s.layerSize * s.epochSize / len(s.smeshers)
}

func (s *session) register(actions ...action) {
	s.actions = append(s.actions, actions...)
}

type tallyAction struct {
	lid    uint32
	before []action
}

func (t *tallyAction) deps() []action {
	return t.before
}

func (t *tallyAction) String() string {
	return fmt.Sprintf("tally %d", t.lid)
}

func (t *tallyAction) execute(trt *Tortoise) {
	trt.TallyVotes(context.Background(), types.LayerID(t.lid))
}

func (s *session) tally(lid int) {
	s.register(&tallyAction{uint32(types.GetEffectiveGenesis()) + uint32(lid), nil})
}

// tallyWait will not be ordered before any other action that was registered
// before tallyWait.
func (s *session) tallyWait(lid int) {
	deps := make([]action, len(s.actions))
	copy(deps, s.actions)
	s.register(&tallyAction{uint32(types.GetEffectiveGenesis()) + uint32(lid), deps})
}

type hareAction struct {
	block *blockAction
}

func (h *hareAction) String() string {
	return fmt.Sprintf("hare %d/%s", h.block.LayerID, h.block.ID)
}

func (h *hareAction) execute(trt *Tortoise) {
	trt.OnHareOutput(h.block.LayerID, h.block.ID)
}

func (h *hareAction) deps() []action {
	return []action{h.block}
}

type blockAction struct {
	types.BlockHeader
}

func (b *blockAction) String() string {
	return fmt.Sprintf("block %+v", b.BlockHeader)
}

func (b *blockAction) execute(trt *Tortoise) {
	trt.OnBlock(b.BlockHeader)
}

func (*blockAction) deps() []action {
	return nil
}

func (s *session) hare(block *blockAction) {
	s.register(&hareAction{block})
}

func (s *session) block(lid int, id string, height uint64) *blockAction {
	header := types.BlockHeader{
		LayerID: types.GetEffectiveGenesis() + types.LayerID(lid),
		Height:  height,
	}
	copy(header.ID[:], id)
	val := &blockAction{header}
	s.register(val)
	return val
}

func (s *session) hareblock(lid int, id string, height uint64) {
	s.hare(s.block(lid, id, height))
}

type beaconAction struct {
	epoch  uint32
	beacon types.Beacon
}

func (b *beaconAction) String() string {
	return fmt.Sprintf("beacon %d / %v", b.epoch, b.beacon)
}

func (*beaconAction) deps() []action {
	return nil
}

func (b *beaconAction) execute(trt *Tortoise) {
	trt.OnBeacon(types.EpochID(b.epoch), b.beacon)
}

// beacon accepts publish epoch and value of the beacon.
func (s *session) beacon(epoch uint32, value string) *beaconAction {
	beacon := &beaconAction{epoch: epoch + 1}
	copy(beacon.beacon[:], value)
	s.register(beacon)
	return beacon
}

type results struct {
	results []result.Layer
}

func (r *results) next(lid int) *results {
	r.results = append(r.results, result.Layer{
		Layer:  types.GetEffectiveGenesis() + types.LayerID(lid),
		Blocks: []result.Block{},
	})
	return r
}

func (r *results) verified(lid int) *results {
	r.results = append(r.results, result.Layer{
		Layer:    types.GetEffectiveGenesis() + types.LayerID(lid),
		Verified: true,
		Blocks:   []result.Block{},
	})
	return r
}

const (
	hare = 1 << iota
	valid
	invalid
	data
	local
)

func (r *results) block(id string, height uint64, fields uint) *results {
	rst := &r.results[len(r.results)-1]
	block := result.Block{
		Valid:   fields&valid > 0,
		Invalid: fields&invalid > 0,
		Hare:    fields&hare > 0,
		Data:    fields&data > 0,
		Local:   fields&local > 0,
	}
	copy(block.Header.ID[:], id)
	block.Header.LayerID = rst.Layer
	block.Header.Height = height
	rst.Blocks = append(rst.Blocks, block)
	return r
}

type updateActions struct {
	tb      testing.TB
	expect  []result.Layer
	actions []action
}

func (u *updateActions) String() string {
	return "updates"
}

func (u *updateActions) deps() []action {
	return u.actions
}

func (u *updateActions) execute(trt *Tortoise) {
	u.tb.Helper()
	updates := trt.Updates()
	for i := range updates {
		trt.OnApplied(updates[i].Layer, updates[i].Opinion)
		// TODO(dshulyak) don't know yet how to implement
		updates[i].Opinion = types.Hash32{}
	}
	require.Equal(u.tb, u.expect, updates)
}

func (s *session) updates(tb testing.TB, expect *results) {
	deps := make([]action, len(s.actions))
	copy(deps, s.actions)
	s.register(&updateActions{tb, expect.results, deps})
}

type modeAction struct {
	tb      testing.TB
	actions []action
	expect  Mode
}

func (m *modeAction) deps() []action {
	return m.actions
}

func (m *modeAction) String() string {
	return fmt.Sprintf("expect mode %s", m.expect)
}

func (m *modeAction) execute(t *Tortoise) {
	require.Equal(m.tb, m.expect, t.Mode())
}

func (s *session) mode(tb testing.TB, m Mode) {
	deps := make([]action, len(s.actions))
	copy(deps, s.actions)
	s.register(&modeAction{tb, deps, m})
}

func (s *session) runInorder() {
	s.runActions(s.actions)
}

func (s *session) runActions(actions []action) {
	trt := s.tortoise()
	for _, a := range actions {
		a.execute(trt)
	}
}

func (s *session) runOn(trt *Tortoise) {
	for _, a := range s.actions {
		a.execute(trt)
	}
}

func (s *session) runRandomTopoN(n int) {
	for i := 0; i < n; i++ {
		s.runRandomTopo()
	}
}

func (s *session) runRandomTopo() {
	// TODO(dshulyak) compute and execute all topological paths
	// or optionally a subset of them if all takes too long
	rst := []action{}
	empty := []action{}
	forward := map[action]map[action]struct{}{}
	reverse := map[action]map[action]struct{}{}
	for _, act := range s.actions {
		for _, dep := range act.deps() {
			if reverse[act] == nil {
				reverse[act] = map[action]struct{}{}
			}
			if forward[dep] == nil {
				forward[dep] = map[action]struct{}{}
			}
			reverse[act][dep] = struct{}{}
			forward[dep][act] = struct{}{}
		}
		if len(act.deps()) == 0 {
			empty = append(empty, act)
		}
	}
	rand.Shuffle(len(empty), func(i, j int) {
		empty[i], empty[j] = empty[j], empty[i]
	})
	for len(empty) > 0 {
		act := empty[0]
		empty = empty[1:]
		rst = append(rst, act)
		for dep := range forward[act] {
			delete(reverse[dep], act)
			if len(reverse[dep]) == 0 {
				delete(reverse, dep)
				empty = append(empty, dep)
			}
		}
		delete(forward, act)
	}
	require.Empty(s.tb, reverse, "all dependencies must be satifisied")
	s.runActions(rst)
}

func (s *session) ensureConfig() {
	if s.config == nil {
		config := DefaultConfig()
		s.config = &config
	}
}

func (s *session) withEpochSize(val uint32) *session {
	s.epochSize = int(val)
	types.SetLayersPerEpoch(val)
	return s
}

func (s *session) withHdist(val uint32) *session {
	s.ensureConfig()
	s.hdist = int(val)
	s.config.Hdist = val
	return s
}

func (s *session) withZdist(val uint32) *session {
	s.ensureConfig()
	s.config.Zdist = val
	return s
}

func (s *session) withWindow(val uint32) *session {
	s.ensureConfig()
	s.config.WindowSize = val
	return s
}

func (s *session) withLayerSize(val uint32) *session {
	s.ensureConfig()
	s.layerSize = int(val)
	s.config.LayerSize = val
	return s
}

func (s *session) withDelay(val uint32) *session {
	s.ensureConfig()
	s.config.BadBeaconVoteDelayLayers = val
	return s
}

func (s *session) tortoise() *Tortoise {
	s.ensureConfig()
	trt, err := New(atxsdata.New(), WithLogger(logtest.New(s.tb)), WithConfig(*s.config))
	require.NoError(s.tb, err)
	return trt
}

func TestSanity(t *testing.T) {
	const n = 2
	s := newSession(t)
	for i := 0; i < n; i++ {
		s.smesher(i).atx(1, new(aopt).height(100).weight(400))
	}
	s.beacon(1, "a")
	for i := 0; i < n; i++ {
		s.smesher(i).atx(1).ballot(1, new(bopt).
			beacon("a").
			totalEligibilities(s.epochEligibilities()).
			eligibilities(s.layerSize/n))
	}
	s.tally(1)
	s.hareblock(1, "aa", 0)
	for i := 0; i < n; i++ {
		s.smesher(i).atx(1).ballot(2, new(bopt).
			eligibilities(s.layerSize/n).
			votes(new(evotes).support(1, "aa", 0)),
		)
	}
	s.hareblock(2, "bb", 0)
	s.tallyWait(2)
	s.updates(t, new(results).
		verified(0).
		verified(1).block("aa", 0, valid|hare|data).
		next(2).block("bb", 0, hare|data),
	)
	t.Run("inorder", func(t *testing.T) { s.runInorder() })
	t.Run("random", func(t *testing.T) { s.runRandomTopoN(100) })
}

func TestDisagreement(t *testing.T) {
	const n = 5
	s := newSession(t)
	for i := 0; i < n; i++ {
		s.smesher(i).atx(1, new(aopt).height(100).weight(400))
	}
	s.beacon(1, "a")
	for i := 0; i < n; i++ {
		s.smesher(i).atx(1).ballot(1, new(bopt).
			beacon("a").
			totalEligibilities(s.epochEligibilities()).
			eligibilities(s.layerSize/n))
	}
	s.hareblock(1, "aa", 0)
	for i := 0; i < n; i++ {
		v := new(evotes)
		if i < n/2 {
			v = v.support(1, "aa", 0)
		} else if i == n/2 {
			v = v.abstain(1)
		} else {
			v = v.against(1, "aa", 0)
		}
		s.smesher(i).atx(1).ballot(2, new(bopt).
			eligibilities(s.layerSize/n).
			votes(v),
		)
	}
	s.tallyWait(1)
	s.updates(t, new(results).
		verified(0).
		next(1).block("aa", 0, hare|data),
	)
	s.runInorder()
}

func TestOpinion(t *testing.T) {
	s := newSession(t)

	s.smesher(0).atx(1, new(aopt).height(100).weight(2000))

	s.beacon(1, "a")
	s.smesher(0).atx(1).ballot(1, new(bopt).
		eligibilities(s.layerSize).
		beacon("a").
		totalEligibilities(s.epochEligibilities()),
	)
	for i := 2; i < s.epochSize; i++ {
		id := strconv.Itoa(i)
		s.hareblock(i-1, id, 0)
		s.smesher(0).atx(1).ballot(i, new(bopt).
			eligibilities(s.layerSize).
			votes(new(evotes).
				base(s.smesher(0).atx(1).ballot(i-1)).
				support(i-1, id, 0)),
		)
	}
	s.tallyWait(s.epochSize)
	s.runRandomTopo()
	s.runInorder()
}

func TestEpochGap(t *testing.T) {
	s := newSession(t).withEpochSize(4)
	for i := 0; i < 2; i++ {
		s.smesher(i).atx(1, new(aopt).height(1).weight(10))
	}
	s.beacon(1, "a")
	for i := 0; i < 2; i++ {
		s.smesher(i).atx(1).ballot(1, new(bopt).
			beacon("a").
			totalEligibilities(s.epochEligibilities()).
			eligibilities(s.layerSize/2))
	}
	rst := new(results).
		verified(0)
	for l := 2; l <= s.epochSize; l++ {
		id := strconv.Itoa(l - 1)
		s.hareblock(l-1, id, 0)
		rst = rst.verified(l-1).block(id, 0, hare|data|valid)
		for i := 0; i < 2; i++ {
			s.smesher(i).atx(1).ballot(l, new(bopt).
				eligibilities(s.layerSize/2).
				votes(new(evotes).
					base(s.smesher(i).atx(1).ballot(l-1)).
					support(l-1, id, 0)),
			)
		}
	}
	for i := s.epochSize; i <= 2*s.epochSize; i++ {
		rst = rst.next(i)
	}
	s.tallyWait(2 * s.epochSize)
	s.updates(t, rst)
	s.runRandomTopo()
}
