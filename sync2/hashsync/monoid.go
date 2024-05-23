package hashsync

type Monoid interface {
	Identity() any
	Op(a, b any) any
	Fingerprint(v any) any
}

type CountingMonoid struct{}

var _ Monoid = CountingMonoid{}

func (m CountingMonoid) Identity() any         { return 0 }
func (m CountingMonoid) Op(a, b any) any       { return a.(int) + b.(int) }
func (m CountingMonoid) Fingerprint(v any) any { return 1 }

type combinedMonoid struct {
	m1, m2 Monoid
}

func CombineMonoids(m1, m2 Monoid) Monoid {
	return combinedMonoid{m1: m1, m2: m2}
}

type CombinedFingerprint struct {
	First  any
	Second any
}

func (m combinedMonoid) Identity() any {
	return CombinedFingerprint{
		First:  m.m1.Identity(),
		Second: m.m2.Identity(),
	}
}

func (m combinedMonoid) Op(a, b any) any {
	ac := a.(CombinedFingerprint)
	bc := b.(CombinedFingerprint)
	return CombinedFingerprint{
		First:  m.m1.Op(ac.First, bc.First),
		Second: m.m2.Op(ac.Second, bc.Second),
	}
}

func (m combinedMonoid) Fingerprint(v any) any {
	return CombinedFingerprint{
		First:  m.m1.Fingerprint(v),
		Second: m.m2.Fingerprint(v),
	}
}

func CombinedFirst[T any](fp any) T {
	cfp := fp.(CombinedFingerprint)
	return cfp.First.(T)
}

func CombinedSecond[T any](fp any) T {
	cfp := fp.(CombinedFingerprint)
	return cfp.Second.(T)
}
