// Package symbols provides generic symbol management for variables and constants.
package symbols

import (
	"sort"

	"github.com/retroenv/retrogolib/set"
)

// Manager provides generic symbol tracking with bank support.
// T is the type of symbol being managed (e.g., *variable or Constant).
type Manager[T any] struct {
	banks []*Bank[T]

	items map[uint16]T
	used  set.Set[uint16]
}

// Bank represents a memory bank containing symbols.
type Bank[T any] struct {
	items map[uint16]T
	used  set.Set[uint16]
}

// New creates a new symbol manager.
func New[T any]() *Manager[T] {
	return &Manager[T]{
		items: make(map[uint16]T),
		used:  set.New[uint16](),
	}
}

// Get returns the item at the given address in this bank.
func (b *Bank[T]) Get(address uint16) (T, bool) {
	item, ok := b.items[address]
	return item, ok
}

// Set sets the item at the given address in this bank.
func (b *Bank[T]) Set(address uint16, item T) {
	b.items[address] = item
}

// Has returns whether an item exists at the given address in this bank.
func (b *Bank[T]) Has(address uint16) bool {
	_, ok := b.items[address]
	return ok
}

// Items returns the internal items map for iteration.
// For getting/setting individual items, use Get/Set methods.
func (b *Bank[T]) Items() map[uint16]T {
	return b.items
}

// Used returns the set of used item addresses for this bank.
func (b *Bank[T]) Used() set.Set[uint16] {
	return b.used
}

// AddBank adds a new bank to the manager.
func (m *Manager[T]) AddBank() {
	m.banks = append(m.banks, &Bank[T]{
		items: make(map[uint16]T),
		used:  set.New[uint16](),
	})
}

// Get returns the item at the given address.
func (m *Manager[T]) Get(address uint16) (T, bool) {
	item, ok := m.items[address]
	return item, ok
}

// Set sets the item at the given address.
func (m *Manager[T]) Set(address uint16, item T) {
	m.items[address] = item
}

// Has returns whether an item exists at the given address.
func (m *Manager[T]) Has(address uint16) bool {
	_, ok := m.items[address]
	return ok
}

// Items returns the internal items map for iteration.
// For getting/setting individual items, use Get/Set methods.
func (m *Manager[T]) Items() map[uint16]T {
	return m.items
}

// Len returns the number of items in the manager.
func (m *Manager[T]) Len() int {
	return len(m.items)
}

// SortedByUint16 returns all items as a slice sorted by a uint16 key.
// The keyFunc extracts the sort key from each item.
func (m *Manager[T]) SortedByUint16(keyFunc func(T) uint16) []T {
	items := make([]T, 0, m.Len())
	for _, item := range m.items {
		items = append(items, item)
	}
	sort.Slice(items, func(i, j int) bool {
		return keyFunc(items[i]) < keyFunc(items[j])
	})
	return items
}

// Used returns the set of used item addresses.
func (m *Manager[T]) Used() set.Set[uint16] {
	return m.used
}

// Banks returns the slice of banks.
func (m *Manager[T]) Banks() []*Bank[T] {
	return m.banks
}

// Bank returns the bank at the given index.
func (m *Manager[T]) Bank(index int) *Bank[T] {
	return m.banks[index]
}

// MarkUsed marks an address as used.
func (m *Manager[T]) MarkUsed(address uint16) {
	m.used.Add(address)
}

// IsUsed returns whether an address is marked as used.
func (m *Manager[T]) IsUsed(address uint16) bool {
	return m.used.Contains(address)
}
