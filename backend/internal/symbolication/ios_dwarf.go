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
)

// MachOInfo contains parsed metadata about a Mach-O or dSYM binary.
type MachOInfo struct {
	UUID string
	Arch string
}

// ExtractMachOMetadata extracts the UUID and CPU architecture from Mach-O binary data.
func ExtractMachOMetadata(data []byte) (MachOInfo, error) {
	reader := bytes.NewReader(data)
	mf, err := macho.NewFile(reader)
	if err != nil {
		// Could be a fat/universal binary (macho.FatFile)
		ff, fatErr := macho.NewFatFile(reader)
		if fatErr != nil {
			return MachOInfo{}, fmt.Errorf("open macho file: %w", err)
		}
		defer ff.Close()
		if len(ff.Arches) == 0 {
			return MachOInfo{}, errors.New("macho fat binary has no architectures")
		}
		// Pick the first arch (e.g. ARM64 or AMD64)
		mf = ff.Arches[0].File
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
func ExtractDSYMBinaryFromZip(zipData []byte) ([]byte, string, error) {
	zr, err := zip.NewReader(bytes.NewReader(zipData), int64(len(zipData)))
	if err != nil {
		return nil, "", fmt.Errorf("read zip: %w", err)
	}

	for _, file := range zr.File {
		// Standard dSYM structure: <BundleName>.dSYM/Contents/Resources/DWARF/<BinaryName>
		if strings.Contains(file.Name, "Contents/Resources/DWARF/") && !file.FileInfo().IsDir() {
			rc, err := file.Open()
			if err != nil {
				return nil, "", fmt.Errorf("open dsym inner file %s: %w", file.Name, err)
			}
			buf, err := io.ReadAll(rc)
			_ = rc.Close()
			if err != nil {
				return nil, "", fmt.Errorf("read dsym inner file %s: %w", file.Name, err)
			}
			return buf, filepath.Base(file.Name), nil
		}
	}

	return nil, "", errors.New("no DWARF binary found in dSYM zip archive")
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
}

// NewIOSSymbolicator creates a new symbolicator from Mach-O binary data.
func NewIOSSymbolicator(data []byte) (*IOSSymbolicator, error) {
	mf, err := macho.NewFile(bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("parse macho: %w", err)
	}

	df, err := mf.DWARF()
	if err != nil {
		_ = mf.Close()
		return nil, fmt.Errorf("read dwarf: %w", err)
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

// SymbolicateAddress looks up an instruction pointer (targetPC) in the DWARF debug information.
func (s *IOSSymbolicator) SymbolicateAddress(targetPC uint64) ResolvedLocation {
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
				if lineEntry.Address == targetPC {
					prevEntry = lineEntry
					found = true
					break
				} else if lineEntry.Address > targetPC && prevEntry.Address != 0 && prevEntry.Address <= targetPC {
					found = true
					break
				}
				prevEntry = lineEntry
			}

			if found && prevEntry.File != nil {
				return ResolvedLocation{
					File:     prevEntry.File.Name,
					Line:     prevEntry.Line,
					Function: s.findFunctionName(targetPC),
					Found:    true,
				}
			}
		}
	}

	// Fallback to checking subprograms if line table didn't resolve
	fnName := s.findFunctionName(targetPC)
	if fnName != "" {
		return ResolvedLocation{
			Function: fnName,
			Found:    true,
		}
	}

	return ResolvedLocation{}
}

func (s *IOSSymbolicator) findFunctionName(targetPC uint64) string {
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
				if name, ok := entry.Val(dwarf.AttrName).(string); ok {
					return DemangleSwift(name)
				}
			}
		}
	}
	return ""
}

// DemangleSwift provides readable formatting for Swift and Objective-C symbols.
func DemangleSwift(mangled string) string {
	if mangled == "" {
		return ""
	}

	// Handle Swift mangling prefixes ($s, _$s, $S, _T)
	clean := strings.TrimPrefix(mangled, "_")
	if strings.HasPrefix(clean, "$s") || strings.HasPrefix(clean, "$S") {
		// Basic Swift demangler heuristic: extracts identifier tokens and transforms into dotted path
		parts := extractSwiftTokens(clean[2:])
		if len(parts) >= 2 {
			return strings.Join(parts, ".") + "()"
		}
	}

	// Objective-C formats: +[Class method] or -[Class method]
	if (strings.HasPrefix(mangled, "+[") || strings.HasPrefix(mangled, "-[")) && strings.HasSuffix(mangled, "]") {
		return mangled
	}

	return mangled
}

func extractSwiftTokens(s string) []string {
	var tokens []string
	i := 0
	for i < len(s) {
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
		tokens = append(tokens, s[i:i+numLen])
		i += numLen
	}
	return tokens
}

var iosStackFrameRegex = regexp.MustCompile(`^(?:\d+\s+)?([A-Za-z0-9_+-]+)\s+(0x[0-9a-fA-F]+)\s+(0x[0-9a-fA-F]+)\s+\+\s+(\d+)`)

// DemangleIOSStackTrace parses each frame of an iOS crash report and replaces addresses
// with resolved source locations when matched with binary base slides.
func DemangleIOSStackTrace(stackTrace string, symbolicator *IOSSymbolicator, baseAddress uint64) string {
	if symbolicator == nil || stackTrace == "" {
		return stackTrace
	}

	lines := strings.Split(stackTrace, "\n")
	for idx, line := range lines {
		matches := iosStackFrameRegex.FindStringSubmatch(strings.TrimSpace(line))
		if len(matches) >= 5 {
			// matches[1]: binary name, matches[2]: pc address, matches[3]: load address, matches[4]: offset
			pcVal, err := strconv.ParseUint(strings.TrimPrefix(matches[2], "0x"), 16, 64)
			if err == nil {
				effectivePC := pcVal
				if baseAddress > 0 && pcVal > baseAddress {
					effectivePC = pcVal - baseAddress
				}
				resolved := symbolicator.SymbolicateAddress(effectivePC)
				if resolved.Found {
					var locStr string
					if resolved.File != "" && resolved.Line > 0 {
						locStr = fmt.Sprintf(" (%s:%d)", filepath.Base(resolved.File), resolved.Line)
					}
					fnStr := resolved.Function
					if fnStr == "" {
						fnStr = matches[1]
					}
					lines[idx] = fmt.Sprintf("%s  %s%s", matches[1], fnStr, locStr)
				}
			}
		}
	}
	return strings.Join(lines, "\n")
}
