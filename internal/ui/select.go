package ui

type selection struct{ set map[int]bool }

func (s *selection) toggle(i int) {
	if s.set == nil {
		s.set = map[int]bool{}
	}
	if s.set[i] {
		delete(s.set, i)
	} else {
		s.set[i] = true
	}
}
func (s *selection) has(i int) bool { return s.set[i] }
func (s *selection) count() int     { return len(s.set) }
func (s *selection) indices() []int {
	out := make([]int, 0, len(s.set))
	for i := range s.set {
		out = append(out, i)
	}
	return out
}
func (s *selection) clear() { s.set = nil }
