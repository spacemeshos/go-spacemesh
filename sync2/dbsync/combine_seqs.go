package dbsync

import (
	"iter"
	"slices"

	"github.com/spacemeshos/go-spacemesh/sync2/types"
)

type generator struct {
	nextFn func() (types.Ordered, error, bool)
	stop   func()
	k      types.Ordered
	err    error
	done   bool
}

func gen(seq types.Seq) *generator {
	var g generator
	g.nextFn, g.stop = iter.Pull2(iter.Seq2[types.Ordered, error](seq))
	return &g
}

func (g *generator) next() (k types.Ordered, err error, ok bool) {
	if g.done {
		return nil, nil, false
	}
	if g.k != nil || g.err != nil {
		k = g.k
		err = g.err
		g.k = nil
		g.err = nil
		return k, err, true
	}
	return g.nextFn()
}

func (g *generator) peek() (k types.Ordered, err error, ok bool) {
	if !g.done && g.k == nil && g.err == nil {
		g.k, g.err, ok = g.nextFn()
		g.done = !ok
	}
	if g.done {
		return nil, nil, false
	}
	return g.k, g.err, true
}

type combinedSeq struct {
	gens    []*generator
	wrapped []*generator
}

// combineSeqs combines multiple ordered sequences into one, returning the smallest
// current key among all iterators at each step.
func combineSeqs(startingPoint types.Ordered, seqs ...types.Seq) types.Seq {
	return func(yield func(types.Ordered, error) bool) {
		var c combinedSeq
		if err := c.begin(startingPoint, seqs); err != nil {
			yield(nil, err)
			return
		}
		c.iterate(yield)
	}
}

func (c *combinedSeq) begin(startingPoint types.Ordered, seqs []types.Seq) error {
	for _, seq := range seqs {
		g := gen(seq)
		k, err, ok := g.peek()
		if !ok {
			continue
		}
		if err != nil {
			return err
		}
		if startingPoint != nil && k.Compare(startingPoint) < 0 {
			c.wrapped = append(c.wrapped, g)
		} else {
			c.gens = append(c.gens, g)
		}
	}
	if len(c.gens) == 0 {
		// all iterators wrapped around
		c.gens = c.wrapped
		c.wrapped = nil
	}
	return nil
}

func (c *combinedSeq) aheadGen() (ahead *generator, aheadIdx int, err error) {
	// remove any exhausted generators
	j := 0
	for i := range c.gens {
		_, _, ok := c.gens[i].peek()
		if ok {
			c.gens[j] = c.gens[i]
			j++
		}
	}
	c.gens = c.gens[:j]
	// if all the generators ha
	if len(c.gens) == 0 {
		if len(c.wrapped) == 0 {
			return nil, 0, nil
		}
		c.gens = c.wrapped
		c.wrapped = nil
	}
	ahead = c.gens[0]
	aheadIdx = 0
	aK, err, _ := ahead.peek()
	if err != nil {
		return nil, 0, err
	}
	for i := 1; i < len(c.gens); i++ {
		curK, err, _ := c.gens[i].peek()
		if err != nil {
			return nil, 0, err
		}
		if curK != nil {
			if curK.Compare(aK) < 0 {
				ahead = c.gens[i]
				aheadIdx = i
				aK = curK
			}
		}
	}
	return ahead, aheadIdx, nil
}

func (c *combinedSeq) iterate(yield func(types.Ordered, error) bool) {
	for {
		g, idx, err := c.aheadGen()
		if err != nil {
			yield(nil, err)
			return
		}
		if g == nil {
			break
		}
		k, err, ok := g.next()
		if err != nil {
			yield(nil, err)
			return
		}
		if !ok {
			c.gens = slices.Delete(c.gens, idx, idx+1)
			continue
		}
		if !yield(k, nil) {
			break
		}
		newK, err, ok := g.peek()
		if !ok {
			// if this iterator is exhausted, it'll be removed by the
			// next aheadGen call
			continue
		}
		if err != nil {
			yield(nil, err)
			return
		}
		if k.Compare(newK) >= 0 {
			// the iterator has wrapped around, move it to the wrapped
			// list which will be used after all the iterators have
			// wrapped around
			c.wrapped = append(c.wrapped, g)
			c.gens = slices.Delete(c.gens, idx, idx+1)
		}
	}
}
