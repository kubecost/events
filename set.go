package events

import (
	"fmt"
	"sync"
)

// set is an implementation prototype for a basic mathematical set data structure. It's API is geared more
// towards a specific use inside the events package rather than a more general purpose use.
type set[T comparable] interface {
	// Adds an item to the set. If the item already exists, it is replaced.
	Add(item T)

	// Remove removes the item from the set if it exists, which returns true. If the item doesn't
	// exist, false is returned.
	Remove(item T) bool

	// RemoveOn accepts a predicate that is run against each item, removing the item on a true
	// return.
	RemoveOn(func(T) bool)

	// RemoveAll removes all of the items in the set
	RemoveAll()

	// Has returns true if the item exists within the set. Otherwise, false.
	Has(item T) bool

	// Length returns the total number of items in the set.
	Length() int

	// ToSlice creates a new slice of size Length(), copies the set elements into the slice, and
	// returns it.
	ToSlice() []T

	// CopyTo accepts a destination slice and writes the contents of the set to it.
	CopyTo([]T) error
}

// lockingSet is a thread-safe implementation of set which leverages a go map for storage.
type lockingSet[T comparable] struct {
	lock sync.Mutex
	m    map[T]struct{}
}

// creates a new set for use with dispatching
func newSet[T comparable]() set[T] {
	return &lockingSet[T]{
		m: make(map[T]struct{}),
	}
}

// Adds an item to the set. If the item already exists, it is replaced.
func (s *lockingSet[T]) Add(item T) {
	s.lock.Lock()
	defer s.lock.Unlock()

	s.m[item] = struct{}{}
}

// Remove removes the item from the set if it exists, which returns true. If the item doesn't
// exist, false is returned.
func (s *lockingSet[T]) Remove(item T) (ok bool) {
	s.lock.Lock()
	defer s.lock.Unlock()

	_, ok = s.m[item]

	if !ok {
		return
	}

	delete(s.m, item)
	return
}

// RemoveOn accepts a predicate that is run against each item, removing the item on a true
// return.
func (s *lockingSet[T]) RemoveOn(predicate func(T) bool) {
	s.lock.Lock()
	defer s.lock.Unlock()

	for k := range s.m {
		if predicate(k) {
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
func (s *lockingSet[T]) Has(item T) bool {
	s.lock.Lock()
	defer s.lock.Unlock()

	_, ok := s.m[item]
	return ok
}

// Length returns the total number of items in the set.
func (s *lockingSet[T]) Length() int {
	s.lock.Lock()
	defer s.lock.Unlock()

	return len(s.m)
}

// ToSlice creates a new slice of size Length(), copies the set elements into the slice, and
// returns it.
func (s *lockingSet[T]) ToSlice() []T {
	s.lock.Lock()
	defer s.lock.Unlock()

	l := len(s.m)
	if l == 0 {
		return nil
	}

	slice := make([]T, l)
	index := 0
	for k := range s.m {
		slice[index] = k
		index++
	}
	return slice
}

// CopyTo accepts a destination slice and writes the contents of the set to it.
func (s *lockingSet[T]) CopyTo(destination []T) error {
	s.lock.Lock()
	defer s.lock.Unlock()

	l := len(s.m)
	if l == 0 {
		return nil
	}

	if len(destination) > l {
		return fmt.Errorf("Destination length(%d) < source length (%d)", len(destination), l)
	}

	index := 0
	for k := range s.m {
		destination[index] = k
		index++
	}
	return nil
}
