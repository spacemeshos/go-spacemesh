package rangesync

import (
	"iter"
	"slices"
)

type generator struct {
	nextFn func() (KeyBytes, bool)
	stop   func()
	k      KeyBytes
	error  SeqErrorFunc
	done   bool
}

func gen(sr SeqResult) *generator {
	g := &generator{error: sr.Error}
	g.nextFn, g.stop = iter.Pull(iter.Seq[KeyBytes](sr.Seq))
	return g
}

func (g *generator) next() (k KeyBytes, ok bool) {
	if g.done {
		return nil, false
	}
	if g.k != nil {
		k = g.k
		g.k = nil
		return k, true
	}
	return g.nextFn()
}

func (g *generator) peek() (k KeyBytes, ok bool) {
	if !g.done && g.k == nil {
		g.k, ok = g.nextFn()
		g.done = !ok
	}
	if g.done {
		return nil, false
	}
	return g.k, true
}

type combinedSeq struct {
	gens    []*generator
	wrapped []*generator
}

// CombineSeqs combines multiple ordered sequences from SeqResults into one, returning the
// smallest current key among all iterators at each step.
// startingPoint is used to check if an iterator has wrapped around. If an iterator yields
// a value below startingPoint, it is considered to have wrapped around.
func CombineSeqs(startingPoint KeyBytes, srs ...SeqResult) SeqResult {
	var err error
	return SeqResult{
		Seq: func(yield func(KeyBytes) bool) {
			var c combinedSeq
			// We clean up even if c.begin() returned an error so that we don't leak
			// any pull iterators that are created before c.begin() failed
			defer c.end()
			// In case if c.begin() succeeds, the error is reset. If yield
			// calls SeqResult's Error function, it will get nil until the
			// iteration is finished.
			if err = c.begin(startingPoint, srs); err != nil {
				return
			}
			err = c.iterate(yield)
		},
		Error: func() error {
			return err
		},
	}
}

func (c *combinedSeq) begin(startingPoint KeyBytes, srs []SeqResult) error {
	for _, sr := range srs {
		g := gen(sr)
		k, ok := g.peek()
		if !ok {
			continue
		}
		if err := g.error(); err != nil {
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

func (c *combinedSeq) end() {
	for _, g := range c.gens {
		g.stop()
	}
	for _, g := range c.wrapped {
		g.stop()
	}
}

func (c *combinedSeq) aheadGen() (ahead *generator, aheadIdx int, err error) {
	// remove any exhausted generators
	j := 0
	for i := range c.gens {
		_, ok := c.gens[i].peek()
		if ok {
			c.gens[j] = c.gens[i]
			j++
		} else if err = c.gens[i].error(); err != nil {
			return nil, 0, err
		}
	}
	c.gens = c.gens[:j]
	// if all the generators have wrapped around, move the wrapped generators
	if len(c.gens) == 0 {
		if len(c.wrapped) == 0 {
			return nil, 0, nil
		}
		c.gens = c.wrapped
		c.wrapped = nil
	}
	ahead = c.gens[0]
	aheadIdx = 0
	aK, _ := ahead.peek()
	if err := ahead.error(); err != nil {
		return nil, 0, err
	}
	for i := 1; i < len(c.gens); i++ {
		curK, ok := c.gens[i].peek()
		if !ok {
			// If not all of the generators have wrapped around, then we
			// already did a successful peek() on this generator above, so it
			// should not be exhausted here.
			// If all of the generators have wrapped around, then we have
			// moved to the wrapped generators, but the generators may only
			// end up in wrapped list after a successful peek(), too.
			// So if we get here, then combinedSeq code is broken.
			panic("BUG: unexpected exhausted generator")
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

func (c *combinedSeq) iterate(yield func(KeyBytes) bool) error {
	for {
		g, idx, err := c.aheadGen()
		if err != nil {
			return err
		}
		if g == nil {
			return nil
		}
		k, ok := g.next()
		if !ok {
			if err := g.error(); err != nil {
				return err
			}
			c.gens = slices.Delete(c.gens, idx, idx+1)
			continue
		}
		if !yield(k) {
			return nil
		}
		newK, ok := g.peek()
		if !ok {
			if err := g.error(); err != nil {
				return err
			}
			// if this iterator is exhausted, it'll be removed by the
			// next aheadGen call
			continue
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
