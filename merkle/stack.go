package merkle

// a basic stack of Nodes
type (
	stack struct {
		top    *node
		length int
	}
	node struct {
		value Node
		prev  *node
	}
)

func newStack() *stack {
	return &stack{nil, 0}
}

func (s *stack) len() int {
	return s.length
}

// top item
func (s *stack) peek() Node {
	if s.length == 0 {
		return nil
	}
	return s.top.value
}

func (s *stack) pop() Node {
	if s.length == 0 {
		return nil
	}
	n := s.top
	s.top = n.prev
	s.length--
	return n.value
}

func (s *stack) push(v Node) {
	n := &node{v, s.top}
	s.top = n
	s.length++
}

func (s *stack) toSlice() []Node {
	l := []Node{}
	for i := s.top; i != nil; i = i.prev {
		l = append(l, i.value)
	}
	return l
}
