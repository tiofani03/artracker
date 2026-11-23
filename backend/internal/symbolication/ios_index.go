package symbolication

import (
	"debug/dwarf"
	"math"
	"sort"
	"strings"
)

// funcRange is a DWARF subprogram's [low, high) address range.
type funcRange struct {
	low, high uint64
	name      string
}

// symEntry is a Mach-O LC_SYMTAB function entry sorted by address.
type symEntry struct {
	addr uint64
	name string
}

// lineRow is one row of a compile unit's line table.
type lineRow struct {
	addr   uint64
	file   string
	line   int
	endSeq bool
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
	// syms holds Mach-O LC_SYMTAB symbols sorted by address as a fallback when
	// DWARF subprograms have no entry for an address (e.g. static C++/KMM libs or
	// truncated __DWARF segments).
	syms []symEntry
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
// at loadAddr, i.e. SymbolicateAddress(pc - loadAddr + TextVMAddr()). A pc below
// loadAddr cannot belong to the image and is reported as not found.
func (s *IOSSymbolicator) SymbolicateImageAddress(pc, loadAddr uint64) ResolvedLocation {
	if pc < loadAddr {
		return ResolvedLocation{}
	}
	return s.SymbolicateAddress(pc - loadAddr + s.TextVMAddr())
}

// addressIndex returns the lazily built index; it is safe for concurrent use.
func (s *IOSSymbolicator) addressIndex() *addressIndex {
	s.indexOnce.Do(func() {
		s.indexBuilds++
		s.index = s.safeBuildAddressIndex()
	})
	return s.index
}

// safeBuildAddressIndex builds the index, falling back to an empty one (every
// lookup then reports not-found) if malformed DWARF makes the build panic. The
// symbolicator is shared across goroutines and the Once is already consumed, so a
// nil index would crash every later call.
func (s *IOSSymbolicator) safeBuildAddressIndex() (idx *addressIndex) {
	defer func() {
		if r := recover(); r != nil {
			idx = &addressIndex{}
		}
	}()
	if s.buildIndexFn != nil {
		return s.buildIndexFn()
	}
	return s.buildAddressIndex()
}

type dwarfSpecInfo struct {
	name        string
	linkageName string
	parent      string
}

type pendingSubprogram struct {
	low, high uint64
	name      string
	specOff   dwarf.Offset
	hasSpec   bool
	origOff   dwarf.Offset
	hasOrig   bool
}

func formatDWARFFuncName(name, linkage, parent string) string {
	if name != "" {
		if parent != "" && !strings.HasPrefix(name, parent+".") && !strings.HasPrefix(name, "+[") && !strings.HasPrefix(name, "-[") {
			return parent + "." + name + "()"
		}
		return name
	}
	return linkage
}

func (s *IOSSymbolicator) buildAddressIndex() *addressIndex {
	idx := &addressIndex{}

	if s.dwarfData != nil {
		specs := make(map[dwarf.Offset]dwarfSpecInfo)
		var pending []pendingSubprogram
		type scope struct {
			tag  dwarf.Tag
			name string
		}
		var stack []scope

		reader := s.dwarfData.Reader()
		for {
			entry, err := reader.Next()
			if err != nil || entry == nil {
				break
			}
			if entry.Tag == 0 {
				if len(stack) > 0 {
					stack = stack[:len(stack)-1]
				}
				continue
			}

			var parentName string
			for i := len(stack) - 1; i >= 0; i-- {
				if stack[i].name != "" && (stack[i].tag == dwarf.TagStructType || stack[i].tag == dwarf.TagClassType || stack[i].tag == dwarf.TagNamespace || stack[i].tag == dwarf.TagInterfaceType || stack[i].tag == dwarf.TagModule) {
					parentName = stack[i].name
					break
				}
			}

			rawName, _ := entry.Val(dwarf.AttrName).(string)
			rawName = strings.TrimPrefix(rawName, "\x01")
			linkage, _ := entry.Val(dwarf.AttrLinkageName).(string)
			linkage = strings.TrimPrefix(linkage, "\x01")
			if rawName != "" || linkage != "" {
				specs[entry.Offset] = dwarfSpecInfo{
					name:        rawName,
					linkageName: linkage,
					parent:      parentName,
				}
			}

			switch entry.Tag {
			case dwarf.TagCompileUnit:
				idx.appendLineRows(s.dwarfData, entry)
			case dwarf.TagSubprogram:
				lowPC, okLow := entry.Val(dwarf.AttrLowpc).(uint64)
				if !okLow || lowPC == 0 {
					break
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
				p := pendingSubprogram{
					low:  lowPC,
					high: highPC,
					name: formatDWARFFuncName(rawName, linkage, parentName),
				}
				if so, ok := entry.Val(dwarf.AttrSpecification).(dwarf.Offset); ok {
					p.specOff = so
					p.hasSpec = true
				}
				if ao, ok := entry.Val(dwarf.AttrAbstractOrigin).(dwarf.Offset); ok {
					p.origOff = ao
					p.hasOrig = true
				}
				pending = append(pending, p)
			}

			if entry.Children {
				scopeName := rawName
				if scopeName != "" && parentName != "" && (entry.Tag == dwarf.TagStructType || entry.Tag == dwarf.TagClassType || entry.Tag == dwarf.TagNamespace || entry.Tag == dwarf.TagInterfaceType || entry.Tag == dwarf.TagModule) {
					scopeName = parentName + "." + scopeName
				}
				stack = append(stack, scope{tag: entry.Tag, name: scopeName})
			}
		}

		for _, p := range pending {
			name := p.name
			if name == "" && p.hasSpec {
				if sp, ok := specs[p.specOff]; ok {
					name = formatDWARFFuncName(sp.name, sp.linkageName, sp.parent)
				}
			}
			if name == "" && p.hasOrig {
				if sp, ok := specs[p.origOff]; ok {
					name = formatDWARFFuncName(sp.name, sp.linkageName, sp.parent)
				}
			}
			if name != "" {
				idx.funcs = append(idx.funcs, funcRange{low: p.low, high: p.high, name: name})
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
	}

	if s.machoFile != nil && s.machoFile.Symtab != nil {
		for _, sym := range s.machoFile.Symtab.Syms {
			// Skip STAB debug entries (0xe0 mask) and zero addresses.
			if sym.Value == 0 || (sym.Type&0xe0) != 0 || sym.Name == "" {
				continue
			}
			idx.syms = append(idx.syms, symEntry{
				addr: sym.Value,
				name: strings.TrimPrefix(sym.Name, "\x01"),
			})
		}
		sort.SliceStable(idx.syms, func(i, j int) bool { return idx.syms[i].addr < idx.syms[j].addr })
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
		rows = append(rows, lineRow{addr: le.Address, file: le.File.Name, line: le.Line, endSeq: le.EndSequence})
	}
	if len(rows) == 0 {
		return
	}
	sort.SliceStable(rows, func(i, j int) bool {
		if rows[i].addr != rows[j].addr {
			return rows[i].addr < rows[j].addr
		}
		return !rows[i].endSeq && rows[j].endSeq
	})
	idx.cus = append(idx.cus, cuLines{min: rows[0].addr, max: rows[len(rows)-1].addr, rows: rows})
}

// lookupLine finds the line row covering pc, respecting DWARF end-of-sequence
// markers so gaps between non-contiguous ranges in one compile unit do not shadow
// another compile unit's code.
func (idx *addressIndex) lookupLine(pc uint64) (lineRow, bool) {
	for k := range idx.cus {
		cu := &idx.cus[k]
		if pc < cu.min || pc > cu.max {
			continue
		}
		rows := cu.rows
		i := sort.Search(len(rows), func(i int) bool { return rows[i].addr >= pc })
		if i < len(rows) && rows[i].addr == pc {
			if !rows[i].endSeq {
				return rows[i], true
			}
			continue
		}
		if i > 0 && !rows[i-1].endSeq {
			return rows[i-1], true
		}
	}
	return lineRow{}, false
}

// lookupFunction returns the name of the subprogram containing pc. When ranges
// overlap, the one starting closest below pc wins (the later in DWARF order on a
// tie); ranges that end at or before pc are skipped. When no DWARF subprogram
// matches, it falls back to the Mach-O LC_SYMTAB symbol table.
func (idx *addressIndex) lookupFunction(pc uint64) (string, bool) {
	fs := idx.funcs
	// First range starting after pc; everything before it has low <= pc.
	i := sort.Search(len(fs), func(i int) bool { return fs[i].low > pc })
	for j := i - 1; j >= 0 && idx.funcMaxHigh[j] > pc; j-- {
		if pc < fs[j].high {
			return fs[j].name, true
		}
	}
	if len(idx.syms) > 0 {
		k := sort.Search(len(idx.syms), func(k int) bool { return idx.syms[k].addr > pc })
		if k > 0 {
			return idx.syms[k-1].name, true
		}
	}
	return "", false
}
