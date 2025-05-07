package events

import (
	"sync"
	"weak"
)

// set is an implementation prototype for a basic mathematical set data structure. It's API is geared more
// towards a specific use inside the events package rather than a more general purpose use.
type set[T any] interface {
	// Adds an item to the set. If the item already exists, it is replaced.
	Add(item *T)

	// Remove removes the item from the set if it exists, which returns true. If the item doesn't
	// exist, false is returned.
	Remove(item *T) bool

	// RemoveOn accepts a predicate that is run against each item, removing the item on a true
	// return.
	RemoveOn(func(*T) bool)

	// RemoveAll removes all of the items in the set
	RemoveAll()

	// Has returns true if the item exists within the set. Otherwise, false.
	Has(item *T) bool

	// Filtered returns a slice of items that match the predicate.
	Filtered(func(*T) bool) []*T

	// RemoveAndFilter accepts two predicates, the first is used to remove items from the set,
	// while the second is used to filter the items in the remaining set.
	RemoveAndFilter(removeOn func(*T) bool, filterOn func(*T) bool) []*T

	// Length returns the total number of items in the set.
	Length() int

	// ToSlice creates a new slice of size Length(), copies the set elements into the slice, and
	// returns it.
	ToSlice() []*T
}

// lockingSet is a thread-safe implementation of set which leverages a go map for storage.
type lockingSet[T any] struct {
	lock sync.Mutex
	m    map[weak.Pointer[T]]struct{}
}

// creates a new set for use with dispatching
func newSet[T any]() set[T] {
	return &lockingSet[T]{
		m: make(map[weak.Pointer[T]]struct{}),
	}
}

// Adds an item to the set. If the item already exists, it is replaced.
func (s *lockingSet[T]) Add(v *T) {
	s.lock.Lock()
	defer s.lock.Unlock()

	item := weak.Make(v)
	s.m[item] = struct{}{}
}

// Remove removes the item from the set if it exists, which returns true. If the item doesn't
// exist, false is returned.
func (s *lockingSet[T]) Remove(v *T) (ok bool) {
	s.lock.Lock()
	defer s.lock.Unlock()

	item := weak.Make(v)
	_, ok = s.m[item]

	if !ok {
		return
	}

	delete(s.m, item)
	return
}

// RemoveOn accepts a predicate that is run against each item, removing the item on a true
// return.
func (s *lockingSet[T]) RemoveOn(predicate func(*T) bool) {
	s.lock.Lock()
	defer s.lock.Unlock()

	for k := range s.m {
		v := k.Value()
		if v == nil {
			delete(s.m, k)
			continue
		}

		if predicate(v) {
			delete(s.m, k)
		}
	}
}

// RemoveAll removes all of the items in the set
func (s *lockingSet[T]) RemoveAll() {
	s.lock.Lock()
	defer s.lock.Unlock()

	for k := range s.m {
		delete(s.m, k)
	}
}

// Has returns true if the item exists within the set. Otherwise, false.
func (s *lockingSet[T]) Has(v *T) bool {
	s.lock.Lock()
	defer s.lock.Unlock()

	item := weak.Make(v)
	_, ok := s.m[item]
	return ok
}

// Length returns the total number of items in the set.
func (s *lockingSet[T]) Length() int {
	s.lock.Lock()
	defer s.lock.Unlock()

	return len(s.m)
}

func (s *lockingSet[T]) RemoveAndFilter(removeOn func(*T) bool, filterOn func(*T) bool) []*T {
	s.lock.Lock()
	defer s.lock.Unlock()

	var slice []*T
	for k := range s.m {
		v := k.Value()
		if v == nil {
			delete(s.m, k)
			continue
		}
		if removeOn(v) {
			delete(s.m, k)
			continue
		}

		if filterOn(v) {
			slice = append(slice, v)
		}
	}

	return slice
}

// ToSlice creates a new slice of size Length(), copies the set elements into the slice, and
// returns it.
func (s *lockingSet[T]) ToSlice() []*T {
	s.lock.Lock()
	defer s.lock.Unlock()

	l := len(s.m)
	if l == 0 {
		return nil
	}

	slice := make([]*T, 0, l)
	for k := range s.m {
		v := k.Value()
		if v == nil {
			delete(s.m, k)
			continue
		}
		slice = append(slice, v)
	}

	return slice
}

// Filtered returns a slice of items that match the predicate.
func (s *lockingSet[T]) Filtered(predicate func(*T) bool) []*T {
	s.lock.Lock()
	defer s.lock.Unlock()

	l := len(s.m)
	if l == 0 {
		return nil
	}

	var slice []*T
	for k := range s.m {
		v := k.Value()
		if v == nil {
			delete(s.m, k)
			continue
		}

		if predicate(v) {
			slice = append(slice, v)
		}
	}
	return slice
}
