package symbols

import (
	"testing"

	"github.com/retroenv/retrogolib/assert"
)

type testItem struct {
	name  string
	value uint16
}

//nolint:funlen // test functions can be long
func TestManager(t *testing.T) {
	t.Run("new manager is initialized", func(t *testing.T) {
		mgr := New[testItem]()

		assert.NotNil(t, mgr)
		assert.Equal(t, 0, mgr.Len())
		assert.Len(t, mgr.Banks(), 0)
	})

	t.Run("set and get item", func(t *testing.T) {
		mgr := New[testItem]()
		item := testItem{name: "TEST", value: 0x1234}

		mgr.Set(0x8000, item)

		got, ok := mgr.Get(0x8000)
		assert.True(t, ok)
		assert.Equal(t, "TEST", got.name)
		assert.Equal(t, uint16(0x1234), got.value)
	})

	t.Run("get non-existent returns false", func(t *testing.T) {
		mgr := New[testItem]()

		_, ok := mgr.Get(0x8000)
		assert.False(t, ok)
	})

	t.Run("has checks existence", func(t *testing.T) {
		mgr := New[testItem]()

		assert.False(t, mgr.Has(0x8000))
		mgr.Set(0x8000, testItem{name: "TEST", value: 0x1234})
		assert.True(t, mgr.Has(0x8000))
	})

	t.Run("items returns map for iteration", func(t *testing.T) {
		mgr := New[testItem]()
		mgr.Set(0x8000, testItem{name: "A", value: 0x1111})
		mgr.Set(0x8001, testItem{name: "B", value: 0x2222})

		items := mgr.Items()
		assert.Len(t, items, 2)
		assert.Equal(t, "A", items[0x8000].name)
		assert.Equal(t, "B", items[0x8001].name)
	})

	t.Run("len returns item count", func(t *testing.T) {
		mgr := New[testItem]()

		assert.Equal(t, 0, mgr.Len())

		mgr.Set(0x8000, testItem{name: "A", value: 0x1111})
		assert.Equal(t, 1, mgr.Len())

		mgr.Set(0x8001, testItem{name: "B", value: 0x2222})
		assert.Equal(t, 2, mgr.Len())
	})

	t.Run("sorted by uint16 key", func(t *testing.T) {
		mgr := New[testItem]()
		mgr.Set(0x8002, testItem{name: "C", value: 0x3333})
		mgr.Set(0x8000, testItem{name: "A", value: 0x1111})
		mgr.Set(0x8001, testItem{name: "B", value: 0x2222})

		sorted := mgr.SortedByUint16(func(t testItem) uint16 { return t.value })

		assert.Len(t, sorted, 3)
		assert.Equal(t, "A", sorted[0].name)
		assert.Equal(t, "B", sorted[1].name)
		assert.Equal(t, "C", sorted[2].name)
	})

	t.Run("mark and check used addresses", func(t *testing.T) {
		mgr := New[testItem]()

		assert.False(t, mgr.IsUsed(0x8000))
		mgr.MarkUsed(0x8000)
		assert.True(t, mgr.IsUsed(0x8000))
	})

	t.Run("used set independent of items", func(t *testing.T) {
		mgr := New[testItem]()

		mgr.MarkUsed(0x8000)
		assert.True(t, mgr.IsUsed(0x8000))
		assert.False(t, mgr.Has(0x8000))

		mgr.Set(0x8001, testItem{})
		assert.True(t, mgr.Has(0x8001))
		assert.False(t, mgr.IsUsed(0x8001))
	})
}

func TestBank(t *testing.T) {
	t.Run("add banks", func(t *testing.T) {
		mgr := New[testItem]()

		assert.Len(t, mgr.Banks(), 0)

		mgr.AddBank()
		mgr.AddBank()
		assert.Len(t, mgr.Banks(), 2)
	})

	t.Run("get bank by index", func(t *testing.T) {
		mgr := New[testItem]()
		mgr.AddBank()
		mgr.AddBank()

		bank0 := mgr.Bank(0)
		bank1 := mgr.Bank(1)

		bank0.Set(0x8000, testItem{name: "BANK0", value: 0x0000})
		bank1.Set(0x8000, testItem{name: "BANK1", value: 0x0001})

		got0, _ := bank0.Get(0x8000)
		got1, _ := bank1.Get(0x8000)
		assert.Equal(t, "BANK0", got0.name)
		assert.Equal(t, "BANK1", got1.name)
	})

	t.Run("bank set get has", func(t *testing.T) {
		mgr := New[testItem]()
		mgr.AddBank()
		bank := mgr.Bank(0)

		item := testItem{name: "TEST", value: 0x1234}
		bank.Set(0x8000, item)

		got, ok := bank.Get(0x8000)
		assert.True(t, ok)
		assert.Equal(t, "TEST", got.name)
		assert.True(t, bank.Has(0x8000))
		assert.False(t, bank.Has(0x8001))
	})

	t.Run("banks are independent", func(t *testing.T) {
		mgr := New[testItem]()
		mgr.AddBank()
		mgr.AddBank()

		bank0 := mgr.Bank(0)
		bank1 := mgr.Bank(1)

		bank0.Set(0x8000, testItem{name: "BANK0", value: 0x0000})
		bank0.Used().Add(0x8000)

		assert.True(t, bank0.Has(0x8000))
		assert.False(t, bank1.Has(0x8000))
		assert.True(t, bank0.Used().Contains(0x8000))
		assert.False(t, bank1.Used().Contains(0x8000))
	})
}

func TestGenericTypes(t *testing.T) {
	t.Run("works with pointer types", func(t *testing.T) {
		type ptrItem struct {
			value int
		}

		mgr := New[*ptrItem]()
		item := &ptrItem{value: 42}
		mgr.Set(0x8000, item)

		got, ok := mgr.Get(0x8000)
		assert.True(t, ok)
		assert.Equal(t, 42, got.value)
	})
}
