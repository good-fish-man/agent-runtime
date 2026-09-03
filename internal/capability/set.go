package capability

import "strings"

// Set keeps capability IDs unique while preserving their insertion order.
type Set struct {
	ids     []string
	members map[string]struct{}
}

func NewSet(ids ...string) *Set {
	set := &Set{members: make(map[string]struct{}, len(ids))}
	set.Add(ids...)
	return set
}

func (s *Set) Add(ids ...string) {
	if s == nil {
		return
	}
	if s.members == nil {
		s.members = make(map[string]struct{}, len(ids))
	}
	for _, id := range ids {
		id = strings.TrimSpace(id)
		if id == "" {
			continue
		}
		if _, exists := s.members[id]; exists {
			continue
		}
		s.members[id] = struct{}{}
		s.ids = append(s.ids, id)
	}
}

func (s *Set) Contains(id string) bool {
	if s == nil {
		return false
	}
	_, exists := s.members[strings.TrimSpace(id)]
	return exists
}

func (s *Set) Remove(ids ...string) {
	removed := make(map[string]struct{}, len(ids))
	for _, id := range ids {
		if id = strings.TrimSpace(id); id != "" {
			removed[id] = struct{}{}
		}
	}
	s.RemoveMatching(func(id string) bool {
		_, exists := removed[id]
		return exists
	})
}

func (s *Set) RemoveMatching(match func(string) bool) {
	if s == nil || match == nil {
		return
	}
	kept := s.ids[:0]
	for _, id := range s.ids {
		if match(id) {
			delete(s.members, id)
			continue
		}
		kept = append(kept, id)
	}
	s.ids = kept
}

func (s *Set) RetainMatching(match func(string) bool) {
	if match == nil {
		return
	}
	s.RemoveMatching(func(id string) bool { return !match(id) })
}

func (s *Set) IDs() []string {
	if s == nil {
		return nil
	}
	return append([]string(nil), s.ids...)
}
