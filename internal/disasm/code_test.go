package disasm

import (
	"testing"

	"github.com/retroenv/retrodisasm/internal/offset"
	"github.com/retroenv/retrogolib/assert"
)

func TestUniqueLabelName_CollisionUsesMappingSuffix(t *testing.T) {
	dis := &Disasm{}
	owners := map[string]*offset.DisasmOffset{}

	first := &offset.DisasmOffset{}
	second := &offset.DisasmOffset{}

	name1 := dis.uniqueLabelName("_func_8000", ParseKey{PC: 0x8000, MappingID: 0x1001}, first, owners)
	assert.Equal(t, "_func_8000", name1)

	name2 := dis.uniqueLabelName("_func_8000", ParseKey{PC: 0x8000, MappingID: 0x1002}, second, owners)
	assert.Equal(t, "_func_8000_m1002", name2)
	assert.True(t, name1 != name2)
}
