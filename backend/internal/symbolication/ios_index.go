package symbolication

import (
	"debug/dwarf"
	"math"
	"sort"
)

// funcRange is a DWARF subprogram's [low, high) address range.
type funcRange struct {
	low, high uint64
	name      string
}

// lineRow is one row of a compile unit's line table.
type lineRow struct {
	addr uint64
	file string
	line int
}

// cuLines is the line table of one compile unit, sorted by address.
type cuLines struct {
	min, max uint64 // address span of rows
	rows     []lineRow
}

// addressIndex holds the DWARF function ranges and per-compile-unit line rows
// sorted by address so each lookup is a binary search instead of a walk over all
// of the DWARF.
type addressIndex struct {
	funcs []funcRange
	// funcMaxHigh[i] is the largest high address among funcs[0..i]; it bounds the
	// backwards walk in lookupFunction.
	funcMaxHigh []uint64
	// cus are in DWARF order: compile units can overlap (e.g. assembly inside a
	// Go package's span), and lookupLine returns the first unit that resolves pc,
	// as the original linear scan did.
	cus []cuLines
}

// TextVMAddr returns the vmaddr of the __TEXT segment (0 if absent). Runtime
// addresses are slid by (loadAddr - TextVMAddr()) relative to the DWARF addresses.
func (s *IOSSymbolicator) TextVMAddr() uint64 {
	if s.machoFile == nil {
		return 0
	}
	if seg := s.machoFile.Segment("__TEXT"); seg != nil {
		return seg.Addr
	}
	return 0
}

// SymbolicateImageAddress symbolicates a runtime address pc of an image loaded
// at loadAddr, i.e. SymbolicateAddress(pc - loadAddr + TextVMAddr()).
func (s *IOSSymbolicator) SymbolicateImageAddress(pc, loadAddr uint64) ResolvedLocation {
	return s.SymbolicateAddress(pc - loadAddr + s.TextVMAddr())
}

// addressIndex returns the lazily built index; it is safe for concurrent use.
func (s *IOSSymbolicator) addressIndex() *addressIndex {
	s.indexOnce.Do(func() {
		s.indexBuilds++
		s.index = s.buildAddressIndex()
	})
	return s.index
}

func (s *IOSSymbolicator) buildAddressIndex() *addressIndex {
	idx := &addressIndex{}

	reader := s.dwarfData.Reader()
	for {
		entry, err := reader.Next()
		if err != nil || entry == nil {
			break
		}

		switch entry.Tag {
		case dwarf.TagCompileUnit:
			idx.appendLineRows(s.dwarfData, entry)
		case dwarf.TagSubprogram:
			lowPC, okLow := entry.Val(dwarf.AttrLowpc).(uint64)
			name, okName := entry.Val(dwarf.AttrName).(string)
			if !okLow || !okName {
				continue
			}
			// A missing high PC leaves the range open-ended, as the original scan did.
			highPC := uint64(math.MaxUint64)
			switch h := entry.Val(dwarf.AttrHighpc).(type) {
			case uint64:
				highPC = h
			case int64:
				highPC = lowPC + uint64(h)
			}
			if highPC == 0 {
				highPC = math.MaxUint64
			}
			idx.funcs = append(idx.funcs, funcRange{low: lowPC, high: highPC, name: name})
		}
	}

	// Stable sort keeps DWARF order among equal addresses, which is what the
	// linear scan returned first.
	sort.SliceStable(idx.funcs, func(i, j int) bool { return idx.funcs[i].low < idx.funcs[j].low })

	idx.funcMaxHigh = make([]uint64, len(idx.funcs))
	var maxHigh uint64
	for i, f := range idx.funcs {
		if f.high > maxHigh {
			maxHigh = f.high
		}
		idx.funcMaxHigh[i] = maxHigh
	}
	return idx
}

func (idx *addressIndex) appendLineRows(data *dwarf.Data, cuEntry *dwarf.Entry) {
	lr, err := data.LineReader(cuEntry)
	if err != nil || lr == nil {
		return
	}
	var rows []lineRow
	var le dwarf.LineEntry
	for {
		if err := lr.Next(&le); err != nil {
			break
		}
		// Rows at address 0 come from functions the linker discarded.
		if le.Address == 0 || le.File == nil {
			continue
		}
		rows = append(rows, lineRow{addr: le.Address, file: le.File.Name, line: le.Line})
	}
	if len(rows) == 0 {
		return
	}
	sort.SliceStable(rows, func(i, j int) bool { return rows[i].addr < rows[j].addr })
	idx.cus = append(idx.cus, cuLines{min: rows[0].addr, max: rows[len(rows)-1].addr, rows: rows})
}

// lookupLine finds the line row covering pc, with the same rules as the original
// linear scan: in the first compile unit that resolves pc, the first row at
// exactly pc, otherwise the closest preceding row provided the unit has a row past
// pc. (Like the original, this means a pc in padding between two sequences
// resolves to the row that ended the earlier sequence.)
func (idx *addressIndex) lookupLine(pc uint64) (lineRow, bool) {
	for k := range idx.cus {
		cu := &idx.cus[k]
		if pc < cu.min || pc > cu.max {
			continue
		}
		rows := cu.rows
		i := sort.Search(len(rows), func(i int) bool { return rows[i].addr >= pc })
		if rows[i].addr == pc {
			return rows[i], true
		}
		if i > 0 {
			return rows[i-1], true
		}
	}
	return lineRow{}, false
}

// lookupFunction returns the name of the innermost subprogram whose range
// contains pc.
func (idx *addressIndex) lookupFunction(pc uint64) (string, bool) {
	fs := idx.funcs
	// First range starting after pc; everything before it has low <= pc.
	i := sort.Search(len(fs), func(i int) bool { return fs[i].low > pc })
	for j := i - 1; j >= 0 && idx.funcMaxHigh[j] > pc; j-- {
		if pc < fs[j].high {
			return fs[j].name, true
		}
	}
	return "", false
}
