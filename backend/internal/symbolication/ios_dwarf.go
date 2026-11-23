package symbolication

import (
	"archive/zip"
	"bytes"
	"debug/dwarf"
	"debug/macho"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
)

// MachOInfo contains parsed metadata about a Mach-O or dSYM binary.
type MachOInfo struct {
	UUID string
	Arch string
}

// openMachOResilient opens a Mach-O single-arch or universal binary, clamping
// a truncated LC_SYMTAB string table if the binary was zipped before dsymutil
// finished flushing the end of the file.
func openMachOResilient(data []byte) (*macho.File, error) {
	reader := bytes.NewReader(data)
	mf, err := macho.NewFile(reader)
	if err == nil {
		return mf, nil
	}
	if ff, fatErr := macho.NewFatFile(reader); fatErr == nil {
		if len(ff.Arches) == 0 {
			_ = ff.Close()
			return nil, errors.New("macho fat binary has no architectures")
		}
		return ff.Arches[0].File, nil
	}
	if patched, ok := clampTruncatedSymtab(data); ok {
		if mf2, err2 := macho.NewFile(bytes.NewReader(patched)); err2 == nil {
			return mf2, nil
		}
	}
	return nil, fmt.Errorf("open macho file: %w", err)
}

func clampTruncatedSymtab(data []byte) ([]byte, bool) {
	if len(data) < 32 {
		return nil, false
	}
	magic := binary.LittleEndian.Uint32(data[0:4])
	off := 32
	if magic == 0xfeedface {
		off = 28
	} else if magic != 0xfeedfacf {
		return nil, false
	}
	ncmds := int(binary.LittleEndian.Uint32(data[16:20]))
	patched := make([]byte, len(data))
	copy(patched, data)
	changed := false
	for i := 0; i < ncmds && off+8 <= len(patched); i++ {
		cmd := binary.LittleEndian.Uint32(patched[off : off+4])
		cmdsize := int(binary.LittleEndian.Uint32(patched[off+4 : off+8]))
		if cmdsize < 8 || off+cmdsize > len(patched) {
			break
		}
		// LC_SYMTAB = 0x2
		if cmd == 0x2 && cmdsize >= 24 {
			stroff := int(binary.LittleEndian.Uint32(patched[off+16 : off+20]))
			strsize := int(binary.LittleEndian.Uint32(patched[off+20 : off+24]))
			if stroff > 0 && stroff < len(patched) && stroff+strsize > len(patched) {
				binary.LittleEndian.PutUint32(patched[off+20:off+24], uint32(len(patched)-stroff))
				patched[len(patched)-1] = 0
				changed = true
			}
		}
		off += cmdsize
	}
	return patched, changed
}

// ExtractMachOMetadata extracts the UUID and CPU architecture from Mach-O binary data.
func ExtractMachOMetadata(data []byte) (MachOInfo, error) {
	mf, err := openMachOResilient(data)
	if err != nil {
		return MachOInfo{}, err
	}
	defer mf.Close()

	var uuidStr string
	for _, load := range mf.Loads {
		raw := load.Raw()
		if len(raw) < 8 {
			continue
		}
		cmd := binary.LittleEndian.Uint32(raw[0:4])
		// LC_UUID = 0x1b
		if cmd == 0x1b && len(raw) >= 24 {
			u := raw[8:24]
			uuidStr = fmt.Sprintf("%02X%02X%02X%02X-%02X%02X-%02X%02X-%02X%02X-%02X%02X%02X%02X%02X%02X",
				u[0], u[1], u[2], u[3],
				u[4], u[5],
				u[6], u[7],
				u[8], u[9],
				u[10], u[11], u[12], u[13], u[14], u[15])
			break
		}
	}

	archStr := "unknown"
	switch mf.Cpu {
	case macho.CpuArm64:
		archStr = "arm64"
	case macho.CpuAmd64:
		archStr = "x86_64"
	case macho.Cpu386:
		archStr = "x86"
	case macho.CpuArm:
		archStr = "arm"
	}

	return MachOInfo{
		UUID: uuidStr,
		Arch: archStr,
	}, nil
}

// ExtractDSYMBinaryFromZip extracts the inner DWARF binary from a zipped .dSYM folder.
// When Xcode 16 Debug builds (ENABLE_DEBUG_DYLIB=YES) produce both a tiny stub
// executable and a large <App>.debug.dylib inside Contents/Resources/DWARF/,
// the largest candidate is selected. If the binary's LC_SYMTAB string table was
// truncated while dsymutil was flushing, it is repaired from the archive's
// Contents/Resources/Relocations/*/*.yml table when present.
func ExtractDSYMBinaryFromZip(zipData []byte) ([]byte, string, error) {
	zr, err := zip.NewReader(bytes.NewReader(zipData), int64(len(zipData)))
	if err != nil {
		return nil, "", fmt.Errorf("read zip: %w", err)
	}

	var bestFile *zip.File
	relocFiles := make(map[string]*zip.File)
	for _, file := range zr.File {
		if file.FileInfo().IsDir() {
			continue
		}
		base := filepath.Base(file.Name)
		if strings.HasPrefix(base, "._") || base == ".DS_Store" {
			continue
		}
		if strings.Contains(file.Name, "Contents/Resources/Relocations/") && strings.HasSuffix(base, ".yml") {
			relocFiles[strings.TrimSuffix(base, ".yml")] = file
			continue
		}
		// Standard dSYM structure: <BundleName>.dSYM/Contents/Resources/DWARF/<BinaryName>
		if strings.Contains(file.Name, "Contents/Resources/DWARF/") {
			if bestFile == nil || file.UncompressedSize64 > bestFile.UncompressedSize64 {
				bestFile = file
			}
		}
	}

	if bestFile == nil {
		return nil, "", errors.New("no DWARF binary found in dSYM zip archive")
	}

	rc, err := bestFile.Open()
	if err != nil {
		return nil, "", fmt.Errorf("open dsym inner file %s: %w", bestFile.Name, err)
	}
	buf, err := io.ReadAll(rc)
	_ = rc.Close()
	if err != nil {
		return nil, "", fmt.Errorf("read dsym inner file %s: %w", bestFile.Name, err)
	}

	baseName := filepath.Base(bestFile.Name)
	if relocFile, ok := relocFiles[baseName]; ok && isMachOSymtabTruncated(buf) {
		if ymlRC, rErr := relocFile.Open(); rErr == nil {
			ymlBytes, yErr := io.ReadAll(ymlRC)
			_ = ymlRC.Close()
			if yErr == nil {
				buf = repairSymtabFromRelocationsYAML(buf, ymlBytes)
			}
		}
	}

	return buf, baseName, nil
}

func isMachOSymtabTruncated(data []byte) bool {
	if len(data) < 32 {
		return false
	}
	magic := binary.LittleEndian.Uint32(data[0:4])
	if magic != 0xfeedfacf {
		return false
	}
	ncmds := int(binary.LittleEndian.Uint32(data[16:20]))
	off := 32
	for i := 0; i < ncmds && off+8 <= len(data); i++ {
		cmd := binary.LittleEndian.Uint32(data[off : off+4])
		cmdsize := int(binary.LittleEndian.Uint32(data[off+4 : off+8]))
		if cmdsize < 8 || off+cmdsize > len(data) {
			break
		}
		if cmd == 0x2 && cmdsize >= 24 {
			stroff := int(binary.LittleEndian.Uint32(data[off+16 : off+20]))
			strsize := int(binary.LittleEndian.Uint32(data[off+20 : off+24]))
			return stroff > 0 && stroff < len(data) && stroff+strsize > len(data)
		}
		off += cmdsize
	}
	return false
}

var relocYAMLLineRe = regexp.MustCompile(`symName:\s*(?:'([^']+)'|([^,\s]+)),\s*(?:symObjAddr:\s*0x[0-9A-Fa-f]+,\s*)?symBinAddr:\s*0x([0-9A-Fa-f]+)`)

func repairSymtabFromRelocationsYAML(dwarf []byte, yml []byte) []byte {
	addrToName := make(map[uint64]string)
	for _, m := range relocYAMLLineRe.FindAllSubmatch(yml, -1) {
		name := string(m[1])
		if name == "" {
			name = string(m[2])
		}
		if addr, err := strconv.ParseUint(string(m[3]), 16, 64); err == nil && name != "" {
			addrToName[addr] = name
		}
	}
	if len(addrToName) == 0 {
		patched, _ := clampTruncatedSymtab(dwarf)
		if len(patched) > 0 {
			return patched
		}
		return dwarf
	}

	ncmds := int(binary.LittleEndian.Uint32(dwarf[16:20]))
	out := make([]byte, len(dwarf), len(dwarf)+1024*1024)
	copy(out, dwarf)
	off := 32
	for i := 0; i < ncmds && off+8 <= len(out); i++ {
		cmd := binary.LittleEndian.Uint32(out[off : off+4])
		cmdsize := int(binary.LittleEndian.Uint32(out[off+4 : off+8]))
		if cmdsize < 8 || off+cmdsize > len(out) {
			break
		}
		if cmd == 0x2 && cmdsize >= 24 {
			symoff := int(binary.LittleEndian.Uint32(out[off+8 : off+12]))
			nsyms := int(binary.LittleEndian.Uint32(out[off+12 : off+16]))
			stroff := int(binary.LittleEndian.Uint32(out[off+16 : off+20]))
			origLen := len(out)
			out = append(out, 0)
			for sIdx := 0; sIdx < nsyms; sIdx++ {
				entryOff := symoff + sIdx*16
				if entryOff+16 > origLen {
					break
				}
				nStrx := int(binary.LittleEndian.Uint32(out[entryOff : entryOff+4]))
				nValue := binary.LittleEndian.Uint64(out[entryOff+8 : entryOff+16])
				strPos := stroff + nStrx
				truncated := strPos >= origLen || bytes.IndexByte(out[strPos:origLen], 0) == -1
				if truncated {
					if name, ok := addrToName[nValue]; ok {
						newStrx := uint32(len(out) - stroff)
						out = append(out, name...)
						out = append(out, 0)
						binary.LittleEndian.PutUint32(out[entryOff:entryOff+4], newStrx)
					} else {
						binary.LittleEndian.PutUint32(out[entryOff:entryOff+4], 0)
					}
				}
			}
			binary.LittleEndian.PutUint32(out[off+20:off+24], uint32(len(out)-stroff))
			break
		}
		off += cmdsize
	}
	return out
}

// ResolvedLocation contains the demangled/symbolicated source information.
type ResolvedLocation struct {
	File     string `json:"file"`
	Line     int    `json:"line"`
	Function string `json:"function"`
	Found    bool   `json:"found"`
}

// IOSSymbolicator inspects a Mach-O DWARF binary to map crash addresses to source lines.
type IOSSymbolicator struct {
	data      []byte
	machoFile *macho.File
	dwarfData *dwarf.Data
	UUID      string
	Arch      string

	indexOnce   sync.Once
	index       *addressIndex
	indexBuilds int // number of times the index was built; tests assert it stays at 1

	buildIndexFn func() *addressIndex // test hook replacing buildAddressIndex
}

// NewIOSSymbolicator creates a new symbolicator from Mach-O binary data.
func NewIOSSymbolicator(data []byte) (*IOSSymbolicator, error) {
	mf, err := openMachOResilient(data)
	if err != nil {
		return nil, fmt.Errorf("parse macho: %w", err)
	}

	df, err := mf.DWARF()
	if err != nil {
		// Allow symbolication via Mach-O LC_SYMTAB when __DWARF is absent or truncated.
		if mf.Symtab == nil || len(mf.Symtab.Syms) == 0 {
			_ = mf.Close()
			return nil, fmt.Errorf("read dwarf: %w", err)
		}
		df = nil
	}

	meta, _ := ExtractMachOMetadata(data)

	return &IOSSymbolicator{
		data:      data,
		machoFile: mf,
		dwarfData: df,
		UUID:      meta.UUID,
		Arch:      meta.Arch,
	}, nil
}

// Close closes any open file handles.
func (s *IOSSymbolicator) Close() error {
	if s.machoFile != nil {
		return s.machoFile.Close()
	}
	return nil
}

// SymbolicateAddress looks up an instruction pointer (targetPC) in the DWARF debug
// information. targetPC is an address in the binary's own (unslid) address space.
// It uses the sorted address index, which is built on first use.
func (s *IOSSymbolicator) SymbolicateAddress(targetPC uint64) ResolvedLocation {
	if s.dwarfData == nil && (s.machoFile == nil || s.machoFile.Symtab == nil) {
		return ResolvedLocation{}
	}

	idx := s.addressIndex()
	fnName := s.findFunctionName(targetPC)
	if row, ok := idx.lookupLine(targetPC); ok {
		return ResolvedLocation{
			File:     row.file,
			Line:     row.line,
			Function: fnName,
			Found:    true,
		}
	}

	// Fall back to the subprogram ranges or symbol table if the line table didn't resolve.
	if fnName != "" {
		return ResolvedLocation{
			Function: fnName,
			Found:    true,
		}
	}

	return ResolvedLocation{}
}

func (s *IOSSymbolicator) findFunctionName(targetPC uint64) string {
	if name, ok := s.addressIndex().lookupFunction(targetPC); ok {
		return DemangleSwift(name)
	}
	return ""
}

// symbolicateAddressLinear is the original linear-scan implementation of
// SymbolicateAddress. It is kept only as the oracle for the index tests.
func (s *IOSSymbolicator) symbolicateAddressLinear(targetPC uint64) ResolvedLocation {
	if s.dwarfData == nil {
		return ResolvedLocation{}
	}

	reader := s.dwarfData.Reader()
	for {
		entry, err := reader.Next()
		if err != nil || entry == nil {
			break
		}

		if entry.Tag == dwarf.TagCompileUnit {
			lr, err := s.dwarfData.LineReader(entry)
			if err != nil || lr == nil {
				continue
			}

			var lineEntry dwarf.LineEntry
			var prevEntry dwarf.LineEntry
			found := false

			for {
				err := lr.Next(&lineEntry)
				if err != nil {
					break
				}
				if lineEntry.Address == targetPC && !lineEntry.EndSequence {
					prevEntry = lineEntry
					found = true
					break
				} else if lineEntry.Address > targetPC && prevEntry.Address != 0 && prevEntry.Address <= targetPC && !prevEntry.EndSequence {
					found = true
					break
				}
				prevEntry = lineEntry
			}

			if found && prevEntry.File != nil {
				return ResolvedLocation{
					File:     prevEntry.File.Name,
					Line:     prevEntry.Line,
					Function: s.findFunctionNameLinear(targetPC),
					Found:    true,
				}
			}
		}
	}

	// Fallback to checking subprograms if line table didn't resolve
	fnName := s.findFunctionNameLinear(targetPC)
	if fnName != "" {
		return ResolvedLocation{
			Function: fnName,
			Found:    true,
		}
	}

	return ResolvedLocation{}
}

func (s *IOSSymbolicator) findFunctionNameLinear(targetPC uint64) string {
	if s.dwarfData != nil {
		reader := s.dwarfData.Reader()
		for {
			entry, err := reader.Next()
			if err != nil || entry == nil {
				break
			}
			if entry.Tag == dwarf.TagSubprogram {
				lowPC, okLow := entry.Val(dwarf.AttrLowpc).(uint64)
				highVal := entry.Val(dwarf.AttrHighpc)

				var highPC uint64
				switch h := highVal.(type) {
				case uint64:
					highPC = h
				case int64:
					highPC = lowPC + uint64(h)
				}

				if okLow && targetPC >= lowPC && (highPC == 0 || targetPC < highPC) {
					if name, ok := entry.Val(dwarf.AttrName).(string); ok && name != "" {
						return DemangleSwift(name)
					}
					if linkage, ok := entry.Val(dwarf.AttrLinkageName).(string); ok && linkage != "" {
						return DemangleSwift(linkage)
					}
				}
			}
		}
	}
	if s.machoFile != nil && s.machoFile.Symtab != nil {
		var bestAddr uint64
		var bestName string
		for _, sym := range s.machoFile.Symtab.Syms {
			if sym.Value == 0 || (sym.Type&0xe0) != 0 || sym.Name == "" {
				continue
			}
			if sym.Value <= targetPC && sym.Value >= bestAddr {
				bestAddr = sym.Value
				bestName = strings.TrimPrefix(sym.Name, "\x01")
			}
		}
		if bestName != "" {
			return DemangleSwift(bestName)
		}
	}
	return ""
}

var swiftPrivateDiscRe = regexp.MustCompile(`^_[0-9A-Fa-f]{30,36}$`)

// DemangleSwift provides readable formatting for Swift, Objective-C, and C++ symbols.
func DemangleSwift(mangled string) string {
	mangled = strings.TrimPrefix(mangled, "\x01")
	if mangled == "" {
		return ""
	}

	// Objective-C formats: +[Class method] or -[Class method] (including .cold.N suffixes)
	if strings.HasPrefix(mangled, "+[") || strings.HasPrefix(mangled, "-[") {
		return mangled
	}

	// Handle Swift mangling prefixes ($s, _$s, $S, _T)
	clean := strings.TrimPrefix(mangled, "_")
	if strings.HasPrefix(clean, "$s") || strings.HasPrefix(clean, "$S") {
		parts := extractSwiftTokens(clean[2:])
		if len(parts) >= 2 {
			return strings.Join(parts, ".") + "()"
		}
	}

	// Handle C++ Itanium mangling (_ZN..., _ZL..., __ZN..., __ZL...)
	if strings.HasPrefix(clean, "_Z") || strings.HasPrefix(clean, "Z") {
		cpp := strings.TrimPrefix(clean, "_")
		if parts := extractCppTokens(cpp[1:]); len(parts) > 0 {
			return strings.Join(parts, "::") + "()"
		}
	}

	return mangled
}

func splitCamelWords(tok string) []string {
	var out []string
	start := 0
	for i := 1; i < len(tok); i++ {
		if tok[i] >= 'A' && tok[i] <= 'Z' && tok[i-1] >= 'a' && tok[i-1] <= 'z' {
			out = append(out, tok[start:i])
			start = i
		}
	}
	if start < len(tok) {
		out = append(out, tok[start:])
	}
	return out
}

func extractSwiftTokens(s string) []string {
	var tokens []string
	var words []string
	addWords := func(w string) {
		for _, cw := range splitCamelWords(w) {
			if len(words) < 26 && cw != "" {
				words = append(words, cw)
			}
		}
	}

	i := 0
	for i < len(s) && len(tokens) < 3 {
		if s[i] == '0' {
			// Word-substituted identifier (Swift 5 mangling)
			i++
			var sb strings.Builder
			for i < len(s) {
				if s[i] == '0' {
					i++
					break
				}
				if s[i] >= '1' && s[i] <= '9' {
					startNum := i
					for i < len(s) && s[i] >= '0' && s[i] <= '9' {
						i++
					}
					numLen, err := strconv.Atoi(s[startNum:i])
					if err != nil || numLen <= 0 || i+numLen > len(s) {
						break
					}
					piece := s[i : i+numLen]
					sb.WriteString(piece)
					addWords(piece)
					i += numLen
				} else if s[i] >= 'a' && s[i] <= 'z' {
					idx := int(s[i] - 'a')
					if idx < len(words) {
						sb.WriteString(words[idx])
					}
					i++
				} else if s[i] >= 'A' && s[i] <= 'Z' {
					idx := int(s[i] - 'A')
					if idx < len(words) {
						sb.WriteString(words[idx])
					}
					i++
					if i >= len(s) || s[i] < '1' || s[i] > '9' {
						break
					}
				} else {
					break
				}
			}
			tok := sb.String()
			if tok != "" && !swiftPrivateDiscRe.MatchString(tok) {
				tokens = append(tokens, tok)
			}
			continue
		}

		// Read numeric length prefix
		startNum := i
		for i < len(s) && s[i] >= '0' && s[i] <= '9' {
			i++
		}
		if startNum == i {
			i++
			continue
		}
		numLen, err := strconv.Atoi(s[startNum:i])
		if err != nil || numLen <= 0 || i+numLen > len(s) {
			break
		}
		tok := s[i : i+numLen]
		i += numLen
		if swiftPrivateDiscRe.MatchString(tok) {
			continue
		}
		addWords(tok)
		tokens = append(tokens, tok)
	}
	return tokens
}

func extractCppTokens(s string) []string {
	var tokens []string
	nested := len(s) > 0 && s[0] == 'N'
	i := 0
	if nested {
		i = 1
	}
	for i < len(s) {
		if nested && s[i] == 'E' {
			break
		}
		// Destructor D0 / D1 / D2
		if s[i] == 'D' && i+1 < len(s) && (s[i+1] == '0' || s[i+1] == '1' || s[i+1] == '2') && len(tokens) > 0 {
			last := tokens[len(tokens)-1]
			tokens = append(tokens, "~"+last)
			break
		}
		startNum := i
		for i < len(s) && s[i] >= '0' && s[i] <= '9' {
			i++
		}
		if startNum == i {
			i++
			continue
		}
		numLen, err := strconv.Atoi(s[startNum:i])
		if err != nil || numLen <= 0 || i+numLen > len(s) {
			break
		}
		tok := s[i : i+numLen]
		i += numLen
		if tok != "_GLOBAL__N_1" && !strings.HasPrefix(tok, "$_") {
			tokens = append(tokens, tok)
		}
		if !nested {
			break
		}
	}
	return tokens
}

var iosStackFrameRegex = regexp.MustCompile(`^(\d+\s+)?([A-Za-z0-9_.+-]+)\s+(0x[0-9a-fA-F]+)\s+(0x[0-9a-fA-F]+)\s+\+\s+(\d+)`)

func isIOSSystemImage(binName string) bool {
	name := strings.TrimSuffix(binName, ".dylib")
	if strings.HasPrefix(name, "libsystem_") || strings.HasPrefix(name, "libobjc") || strings.HasPrefix(name, "libc++") || strings.HasPrefix(name, "libdispatch") || strings.HasPrefix(name, "libdyld") || strings.HasPrefix(name, "libswift") {
		return true
	}
	switch name {
	case "UIKitCore", "UIKit", "CoreFoundation", "Foundation", "QuartzCore", "CoreAutoLayout",
		"GraphicsServices", "CFNetwork", "Security", "CoreGraphics", "AudioToolbox", "AVFoundation",
		"WebKit", "JavaScriptCore", "Metal", "CoreLocation", "CoreData", "SwiftUI", "Combine", "dyld":
		return true
	}
	return false
}

// DemangleIOSStackTrace parses each frame of an iOS crash report and replaces addresses
// with resolved source locations when matched with binary base slides.
func DemangleIOSStackTrace(stackTrace string, symbolicator *IOSSymbolicator, baseAddress uint64) string {
	if symbolicator == nil || stackTrace == "" {
		return stackTrace
	}

	lines := strings.Split(stackTrace, "\n")
	for idx, line := range lines {
		matches := iosStackFrameRegex.FindStringSubmatch(strings.TrimSpace(line))
		if len(matches) >= 6 {
			// matches[1]: frame prefix, matches[2]: binary name, matches[3]: pc address, matches[4]: load address, matches[5]: offset
			prefix := matches[1]
			binName := matches[2]
			if isIOSSystemImage(binName) {
				continue
			}
			pcVal, err := strconv.ParseUint(strings.TrimPrefix(matches[3], "0x"), 16, 64)
			loadAddr, _ := strconv.ParseUint(strings.TrimPrefix(matches[4], "0x"), 16, 64)
			if err == nil {
				var resolved ResolvedLocation
				if loadAddr > 0 {
					resolved = symbolicator.SymbolicateImageAddress(pcVal, loadAddr)
				} else {
					effectivePC := pcVal
					if baseAddress > 0 && pcVal > baseAddress {
						effectivePC = pcVal - baseAddress
					}
					resolved = symbolicator.SymbolicateAddress(effectivePC)
				}
				if resolved.Found {
					var locStr string
					baseFile := filepath.Base(resolved.File)
					if resolved.File != "" && resolved.Line > 0 {
						locStr = fmt.Sprintf(" (%s:%d)", baseFile, resolved.Line)
					} else if resolved.File != "" && baseFile != "." && !strings.Contains(baseFile, "<compiler-generated>") {
						locStr = fmt.Sprintf(" (%s)", baseFile)
					}
					fnStr := resolved.Function
					if fnStr == "" {
						fnStr = binName
					}
					lines[idx] = fmt.Sprintf("%s%s  %s%s", prefix, binName, fnStr, locStr)
				}
			}
		}
	}
	return strings.Join(lines, "\n")
}
