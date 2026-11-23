# Pull Request: Fix iOS dSYM Symbolication, Multipart Artifact Upload, and Crash Culprit Frame Grouping

## Summary

This PR addresses several critical issues affecting iOS crash reporting and symbolication:
1. **Multipart Upload Unwrapping**: Artifact uploads sent via `multipart/form-data` were previously stored with MIME boundary headers, corrupting zip/gzip decompression and falsely misclassifying dSYMs as Android ProGuard mappings.
2. **Xcode 16 / Debug Dylib Support**: Apps built with `ENABLE_DEBUG_DYLIB=YES` produce a tiny 12 KB stub executable alongside a ~281 MB `Alfagift.debug.dylib` under `Contents/Resources/DWARF/`. The dSYM extractor now extracts the largest debug dylib.
3. **Resilient Mach-O Symbol Table & Relocations**: Handled truncated `LC_SYMTAB` string tables and leveraged DWARF `DW_AT_specification`, `DW_AT_abstract_origin`, and `DW_TAG_module` to resolve symbols where methods are split across compile units.
4. **Swift 5 & C++ Itanium Demangling**: Implemented pure-Go Swift 5 demangling (`$s...` / `_$s...`) with token and word substitution support, demangling Swift symbols like `$s8Alfagift20BasketViewController...` into human-readable `Alfagift.BasketViewController.updateCheckoutView()`.
5. **ASLR Address Calculation**: Accurate translation from runtime instruction pointer addresses to DWARF compile-time virtual addresses using image load addresses.
6. **Smart Crash Culprit & Framework Grouping**: Classified crash reporter frames (`PLCrashReporter`, `FIRCLS`, `FBSDK`, `MoEngage`, `TerminateHandler::`, etc.) and system libraries as framework/library frames, automatically collapsing them and surfacing the actual application culprit frame in the Web UI.

---

## Changes

### 1. Backend (`backend/`)
- **`internal/handler/artifact_body.go` & `artifacts.go`**:
  - Implemented `readArtifactUpload` with multipart form-data parsing, extracting form fields (`name`, `version`, `build`) and file contents (`file`, `artifact`, `mapping`).
  - Added magic header auto-detection (Zip `PK\x03\x04`, Gzip `\x1f\x8b`, ProGuard text detection) to prevent binary artifacts from being erroneously classified as ProGuard mappings.
- **`internal/symbolication/ios_dwarf.go`**:
  - `ExtractDSYMBinaryFromZip`: Iterates through all entries in `Contents/Resources/DWARF/` and picks the largest debug binary (e.g., `Alfagift.debug.dylib`).
  - `openMachOResilient`: Resilient Mach-O parser handling truncated string tables.
  - `DemangleSwift`: Added Swift 5 demangler supporting substitution dictionaries, type prefixes, and identifiers, plus C++ Itanium fallback.
  - `DemangleIOSStackTrace`: Updated stack trace parsing to calculate ASLR offset using `loadAddr`.
- **`internal/symbolication/ios_index.go`**:
  - Enriched DWARF index traversal with `DW_AT_specification`, `DW_AT_abstract_origin`, and `DW_TAG_module`.
  - Added fallback to Mach-O `LC_SYMTAB` when DWARF subprogram attributes are absent.
  - Respected `EndSequence` line markers to prevent false positive compilation unit range overlap.
- **`internal/issues/stackframes.go`**:
  - Enhanced iOS stack trace frame regex (`Frame <N> (<addr>): <symbol> + <offset>` and `<N> <Binary> <PC> <Base> + <Offset>`).
  - Classified crash reporter internals (`PLCrashReporter`, `FIRCLS`, etc.) as non-app/library frames.
- **`internal/handler/issues.go`**:
  - Increased database context timeout to 30 seconds for cold dSYM initial indexing.
  - Ensured rows cursor is closed before symbolication processing.
- **`cmd/api/main.go`**:
  - Loaded `.env` file first before falling back to `../.env`.
  - Adjusted API `WriteTimeout` to accommodate larger artifact processing.

### 2. Frontend (`frontend/`)
- **`src/issues/stackTrace.ts`**:
  - Added classification rules recognizing `PLCrashReporter`, `FIRCLS`, `MoEngage`, `FBSDKCoreKit`, and `TerminateHandler::` as framework/library frames.
  - Enabled recursive consecutive library frame collapsing.
- **`src/pages/IssueDetailPage.tsx`**:
  - Added fallback header display `Crashed in <culprit method>` when no traditional exception header (e.g., `Fatal Exception: ...`) exists in the crash report.
  - Fixed stack trace visual hierarchy to highlight the first application frame.

---

## Root Causes Solved

| Problem | Root Cause | Fix |
|---|---|---|
| dSYM uploaded as ProGuard mapping | Multipart boundary header prepended to file body, causing zip validation failure | Added `readArtifactUpload` multipart unwrapping and magic byte validation |
| Stack trace still showing raw hex / unmapped | DWARF binary extracted was the 12 KB stub instead of the 281 MB `Alfagift.debug.dylib` | Updated `ExtractDSYMBinaryFromZip` to pick largest binary |
| Symbol names mangled (e.g. `$s8Alfagift...`) | Absence of Swift 5 name demangling in pure Go backend | Implemented Swift 5 demangler and Mach-O symbol table fallback |
| Top 35 frames showing `PLCrashReporter` | Stack trace parser treated crash handler frames as app code, hiding the real culprit | Added framework frame classification to group and collapse crash handlers |

---

## How to Test

1. **Unit Tests**:
   ```bash
   cd backend && go test ./internal/symbolication/... ./internal/issues/...
   cd frontend && npm test -- --run src/issues/stackTrace.test.ts
   ```
2. **End-to-End Symbolication**:
   - Upload iOS dSYM zip file via `POST /api/v1/projects/:id/artifacts` (both raw binary or `multipart/form-data`).
   - Trigger an iOS crash event (or verify existing Issue #3 with `Alfagift`).
   - Check Issue Detail page:
     - Header displays `Crashed in Alfagift.BasketViewController.updateCheckoutView()`
     - Frames 0–34 (PLCrashReporter / system libs) are collapsed into one expander.
     - Frame 35 is highlighted as the crash culprit with line information.

---

## Visual Evidence / Screenshots

| Before (Unmapped / Raw hex & Crash reporter at top) | After (Symbolicated & Culprit Highlighted) |
|---|---|
| Frame 0: `-[PLCrashReporter enableCrashReporterAndReturnError:]` <br> Frame 35: `0x0000000112a1c9fc 0x111a8c000 + 16321020` | Header: `Crashed in Alfagift.BasketViewController.updateCheckoutView()` <br> Frames 0–34 collapsed <br> Frame 35: `Alfagift.BasketViewController.updateCheckoutView() + 188` |
