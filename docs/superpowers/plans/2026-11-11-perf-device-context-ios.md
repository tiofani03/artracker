# Perf Device Context — iOS SDK Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add OS version, free/total RAM, free/total storage and the active connection type to the iOS `perf.samples` items, so the dashboard's Performance table can show them.

**Architecture:** Extend the perf pipeline from `2026-11-11-session-profiling-ios.md`. Mach and file-system readers extend `Support/Metrics.swift` as pure, injectable functions. The existing `Support/Reachability.swift` gains an interface-type reading. `ProcessProbe` carries the new readers, `PerfSample` gets three optional fields and the `device` block gets two. Nothing is added to `session.start`.

**Tech Stack:** Swift 5.9, SPM, iOS 15 / macOS 12, XCTest. Pure logic is tested on macOS with `swift test`; anything using `NWPath` or UIKit stays behind small seams.

**Spec:** `artracker/docs/superpowers/specs/2026-11-11-session-profiling-design.md`. The wire contract is §3 and the metric definitions are §4 (rows `ram_free_mb`, `storage_free_mb`, `net`, `device.storage_mb`, `device.os_version`). The backend already accepts these fields.

**Repo:** `~/AndroidStudioProjects/github/artracker-ios-sdk`. Paths below are relative to `Sources/ARTracker/` (SRC) and `Tests/ARTrackerTests/` (TEST).

## Prerequisite

The perf classes (`SRC/Performance/…`) were **not present** in the local checkout on `main` when this plan was written (`SRC/Performance/` does not exist), although perf batches are already arriving from an iOS build. Before Task 1, locate the branch or clone that has them, or finish `2026-11-11-session-profiling-ios.md` first. Names below follow that plan (`ProcessProbe`, `PerfSample`, `PerfSampler`, `PerfMonitor`); if the real names differ, keep the signatures and rename.

## Global Constraints

- New wire fields, exactly: `device.os_version` (string, ≤ 32 chars), `device.storage_mb` (int), sample `ram_free_mb` (number, MB), sample `storage_free_mb` (number, MB), sample `net` (`"wifi"` | `"cellular"` | `"ethernet"` | `"none"`).
- `ram_mb` stays `ProcessInfo.physicalMemory` in MB. `ram_free_mb` is system-wide free memory: `(free + inactive + speculative)` pages × page size from `host_statistics64`, in MB. It is an approximation of what the system can hand out and is **not** `os_proc_available_memory()` (that is per-app headroom).
- `os_version` is `ProcessInfo.operatingSystemVersion` formatted `major.minor` and `.patch` only when patch ≠ 0 (`"26.4"`, `"17.4.1"`). Never the long `operatingSystemVersionString` ("Version 26.4 (Build …)").
- Storage reads the home directory volume: `volumeAvailableCapacityForImportantUsageKey` for free, `volumeTotalCapacityKey` for total. MB means `bytes / 1_048_576`, rounded to the nearest integer; all new numeric fields are sent as integers.
- A reading that fails returns `nil` and the field is **omitted**, never 0 and never `"unknown"`. `net` is `"none"` only when the path is not satisfied.
- Reading must not run on the main thread: the monitor's utility queue does it.
- Everything new is `internal`. No new public API, no new entitlement. Reachability keeps its public surface and only adds one member.
- Test command: `swift test`. Filter with `swift test --filter <Class>`. UIKit-only code stays in `#if canImport(UIKit)`.

## Review Focus

1. **Path unsatisfied vs. unknown interface.** An unsatisfied path is `"none"`; a satisfied path on an interface that is none of wifi, cellular or wired (loopback, other) omits `net`. Covered by Task 2 tests `unsatisfied path is none` and `other interface omits net`.
2. **Reachability not started.** If perf is on but `Reachability.start()` was never called, the probe must start it (idempotent) or omit `net`, not report a stale `none`. Covered by Task 2 test `net is nil until reachability has a path`.
3. **`host_statistics64` fails.** Returns a non-`KERN_SUCCESS` code on some sandboxes. The sample still goes out without `ram_free_mb`. Covered by Task 3 test `PerfSamplerTests.failed vm statistics omits ram free`.
4. **Patch version zero.** `26.4.0` must be sent as `"26.4"`, not `"26.4.0"`. Covered by Task 1 test `os version drops zero patch`.
5. **Storage keys unavailable.** `resourceValues` can return `nil` capacities; send neither storage field rather than 0. Covered by Task 1 test `missing capacities omit storage`.

---

### Task 1: Memory, storage and OS readings in `Metrics`

**Files:**
- Modify: `SRC/Support/Metrics.swift`
- Create: `SRC/Performance/DeviceFacts.swift`
- Test: `TEST/DeviceFactsTests.swift`

**Interfaces:**
- Produces, in `DeviceFacts.swift` (all pure, no system calls):
  - `func freeRamMb(freePages: UInt64, inactivePages: UInt64, speculativePages: UInt64, pageSize: UInt64) -> Int`
  - `func storageMb(bytes: Int64?) -> Int?` (nil stays nil)
  - `func osVersionString(major: Int, minor: Int, patch: Int) -> String`
- Produces, in `Metrics.swift`:
  - `static func freeRamBytesSample() -> (free: UInt64, inactive: UInt64, speculative: UInt64, pageSize: UInt64)?` (nil on non-success from `host_statistics64`)
  - `static func storageCapacities() -> (freeBytes: Int64?, totalBytes: Int64?)` reading the home directory volume
  - `static func osVersion() -> String` built with `osVersionString` from `ProcessInfo.processInfo.operatingSystemVersion`

- [ ] **Step 1: Write the failing tests** in `DeviceFactsTests`
  - `free ram sums free inactive and speculative`: `freeRamMb(freePages: 10_000, inactivePages: 5_000, speculativePages: 1_000, pageSize: 16_384)` equals `250` (16_000 × 16_384 / 1_048_576 = 250).
  - `os version drops zero patch`: `osVersionString(major: 26, minor: 4, patch: 0) == "26.4"` and `osVersionString(major: 17, minor: 4, patch: 1) == "17.4.1"`.
  - `missing capacities omit storage`: `storageMb(bytes: nil) == nil` and `storageMb(bytes: 107_374_182_400) == 102_400`.

- [ ] **Step 2: Run** `swift test --filter DeviceFactsTests`. Expected: FAIL (functions not defined).

- [ ] **Step 3: Implement** the three pure functions, then the `Metrics` readers. `storageCapacities` uses `URL(fileURLWithPath: NSHomeDirectory()).resourceValues(forKeys:)` with the two keys named in Global Constraints. `freeRamBytesSample` calls `host_statistics64(mach_host_self(), HOST_VM_INFO64, …)` and returns `nil` unless it returns `KERN_SUCCESS`.

- [ ] **Step 4: Run** `swift test --filter DeviceFactsTests`. Expected: PASS.

- [ ] **Step 5: Commit** `feat(perf): free RAM, storage and OS version readers`.

---

### Task 2: Connection type from `Reachability`

**Files:**
- Modify: `SRC/Support/Reachability.swift`
- Create: `SRC/Performance/NetKind.swift`
- Test: `TEST/NetKindTests.swift`

**Interfaces:**
- Produces:
  - `enum NetKind { static func from(satisfied: Bool, wifi: Bool, cellular: Bool, wired: Bool) -> String? }`. Unsatisfied gives `"none"`; satisfied checks wifi, then cellular, then wired (`"ethernet"`); none of them gives `nil`.
  - `Reachability.interfaceKind: String?` (thread-safe through the existing `lock`). Set in `pathUpdateHandler` from `path.status == .satisfied`, `path.usesInterfaceType(.wifi)`, `.cellular`, `.wiredEthernet`. `nil` until the first path update.
- Consumes: the existing `Reachability.shared.start()` (idempotent).

- [ ] **Step 1: Write the failing tests** in `NetKindTests`
  - `unsatisfied path is none`: `NetKind.from(satisfied: false, wifi: true, cellular: false, wired: false) == "none"`.
  - `interface mapping`: wifi gives `"wifi"`, cellular gives `"cellular"`, wired gives `"ethernet"`; with both wifi and cellular flags set the result is `"wifi"`.
  - `other interface omits net`: `NetKind.from(satisfied: true, wifi: false, cellular: false, wired: false) == nil`.
  - `net is nil until reachability has a path`: a fresh `Reachability()` has `interfaceKind == nil`.

- [ ] **Step 2: Run** `swift test --filter NetKindTests`. Expected: FAIL.

- [ ] **Step 3: Implement** `NetKind` and the `Reachability` property. `PerfMonitor` start (Task 3) calls `Reachability.shared.start()`.

- [ ] **Step 4: Run** `swift test --filter NetKindTests`. Expected: PASS.

- [ ] **Step 5: Commit** `feat(perf): connection type from Reachability`.

---

### Task 3: Wire fields into `ProcessProbe`, `PerfSample` and the `device` block

**Files:**
- Modify: `SRC/Performance/PerfSample.swift`, `SRC/Performance/PerfSampler.swift`, `SRC/Performance/PerfMonitor.swift`
- Modify: `TEST/PerfSamplerTests.swift`, `TEST/PerfMonitorTests.swift`, `TEST/GoldenJSONTests.swift`

**Interfaces:**
- Consumes: Task 1 readers and Task 2 `Reachability.interfaceKind`.
- Produces:
  - `ProcessProbe` gains closures `ramFreeMb: () -> Int?`, `storageFreeMb: () -> Int?`, `net: () -> String?`. Production values map `Metrics.freeRamBytesSample()` through `freeRamMb`, `Metrics.storageCapacities().freeBytes` through `storageMb`, and `Reachability.shared.interfaceKind`.
  - `PerfSample` gains `ramFreeMb: Int?`, `storageFreeMb: Int?`, `net: String?`; `toJSON()` writes `ram_free_mb`, `storage_free_mb`, `net` and omits nil.
  - The `device` object in `PerfMonitor` gains `os_version` (from `Metrics.osVersion()`, truncated to 32 chars, omitted when empty) and `storage_mb` (from `Metrics.storageCapacities().totalBytes` via `storageMb`, omitted when nil).

- [ ] **Step 1: Write the failing tests**
  - `PerfSamplerTests.sample carries ram storage and net`: probe closures return `2310`, `51200`, `"wifi"`; the sample has them and `toJSON()` has keys `ram_free_mb`, `storage_free_mb`, `net` with those values.
  - `PerfSamplerTests.failed vm statistics omits ram free`: the production probe's `ramFreeMb` returns `nil` when the injected `freeRamBytesSample` returns `nil`, and the other two fields are still present in the sample.
  - `PerfSamplerTests.nil readings are omitted`: closures return `nil`; `toJSON()` has none of the three keys.
  - `PerfMonitorTests.device object has os version and storage`: the flushed item's `attributes.device` equals `{ram_mb, storage_mb: 128000, os_version: "26.4", cores, refresh_hz}` for the fake values.
  - `PerfMonitorTests.empty os version is omitted`: `os_version` closure returns `""`; `device` has no `os_version` key.
  - `GoldenJSONTests`: update the perf golden to include the three sample keys and the two device keys.

- [ ] **Step 2: Run** `swift test --filter PerfSamplerTests --filter PerfMonitorTests --filter GoldenJSONTests`. Expected: FAIL.

- [ ] **Step 3: Implement** the model, probe and monitor changes. The monitor calls `Reachability.shared.start()` when it starts sampling.

- [ ] **Step 4: Run** `swift test`. Expected: all green.

- [ ] **Step 5: Verify against the backend contract.** Print one flushed item's `attributes` JSON from the golden test and confirm every key and the `net` values match Global Constraints exactly (the backend drops anything else without an error).

- [ ] **Step 6: Commit** `feat(perf): send os version, RAM, storage and network type`.

---

### Task 4: Docs and demo

**Files:**
- Modify: `README.md` ("Performance monitoring" section)
- Modify: the demo only if it prints perf attributes

- [ ] **Step 1:** Document the five new fields, that they come from `perf` and not `session.start`, and that `ram_free_mb` is system-wide free memory.
- [ ] **Step 2:** Run `swift test`. Expected: PASS.
- [ ] **Step 3:** Run a demo app on a device with performance monitoring on, open the dashboard replay for that session, and confirm the Performance table shows OS, RAM, Storage and Network rows (see the note below).
- [ ] **Step 4: Commit** `docs(perf): device context fields`.

## Dashboard check (outside this repo)

The dashboard side already ships in `artracker` (`PerformancePanel.tsx`, backend `ingest/perf.go`). It shows the new rows only after that backend build is deployed. If the table stays empty after Task 4 Step 3, query `session_perf_batches` for the session and check whether `device`/`samples` contain the new keys: present means a frontend problem, absent means the deployed backend is older than the whitelist change or the SDK sent different names.
