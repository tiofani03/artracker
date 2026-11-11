# Perf Device Context — Android SDK Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add OS version, free/total RAM, free/total storage and the active connection type to the Android `perf.samples` items, so the dashboard's Performance table can show them.

**Architecture:** Extend the existing perf pipeline from `2026-11-11-session-profiling-android.md`. `DeviceProbe` gains the new readings and a new `NetworkProbe` caches the default network type from a `ConnectivityManager.NetworkCallback`. `PerfSample` gets three optional fields and the `device` block gets two. Nothing is added to `session.start`.

**Tech Stack:** Kotlin 2.0.21, minSdk 24, `explicitApi()`, JUnit4 + Robolectric 4.14.1 (`@Config(sdk = [34])`), hand-written fakes in `src/test/.../testutil`.

**Spec:** `artracker/docs/superpowers/specs/2026-11-11-session-profiling-design.md`. The wire contract is §3 and the metric definitions are §4 (rows `ram_free_mb`, `storage_free_mb`, `net`, `device.storage_mb`, `device.os_version`). The backend already accepts these fields.

**Repo:** `~/AndroidStudioProjects/github/artracker-android-sdk`. Paths below are relative to `artracker-sdk/src/main/java/id/art/tracker/` (SDK) and `artracker-sdk/src/test/java/id/art/tracker/` (TEST).

## Prerequisite

The perf classes (`SDK/perf/DeviceProbe.kt`, `PerfSample.kt`, `PerfSampler.kt`, `PerfMonitor.kt`) were **not present** in the local checkout on `main` when this plan was written (`SDK/perf/` does not exist), although perf batches are already arriving from an Android build. Before Task 1, locate the branch or clone that has them, or finish `2026-11-11-session-profiling-android.md` first. Names below follow that plan; if the real names differ, keep the signatures and rename.

## Global Constraints

- New wire fields, exactly: `device.os_version` (string, ≤ 32 chars), `device.storage_mb` (int), sample `ram_free_mb` (number, MB), sample `storage_free_mb` (number, MB), sample `net` (`"wifi"` | `"cellular"` | `"ethernet"` | `"none"`).
- `ram_mb` stays `ActivityManager.MemoryInfo.totalMem` in MB. `ram_free_mb` is `availMem` in MB (system-wide).
- Any probe that cannot read a value returns `null` and the field is **omitted**, never 0 and never `"unknown"`. `net` is `"none"` only when there is no active network.
- `storage_free_mb` and `device.storage_mb` read the data volume: `StatFs(Environment.getDataDirectory().path)`. MB means `bytes / 1_048_576`, rounded to the nearest integer; all new numeric fields are sent as integers.
- Reading the new values must not throw and must not run on the main thread (the sampler loop already runs on the core scope).
- `NetworkProbe` registers its callback only when `performance.enabled` is true and unregisters it when the monitor stops. When performance is disabled nothing new is created.
- Everything new is `internal`. No new public API, no new permission (`ACCESS_NETWORK_STATE` is already used by `DeviceContext`).
- Test command: `./gradlew :artracker-sdk:testDebugUnitTest`. Full check: `./gradlew :artracker-sdk:testDebugUnitTest :app:testDebugUnitTest :app:assembleDebug`.

## Review Focus

1. **No network at all.** With no active network, `net` must be `"none"`, not omitted and not a crash. Covered by Task 2 test `no active network reports none`.
2. **Unknown transport (VPN, Bluetooth, satellite).** Must omit `net`, not send `"unknown"`, because the backend would drop an unknown value anyway. Covered by Task 2 test `unknown transport omits net`.
3. **Callback leak.** Stopping the monitor (background, SDK disabled) must unregister the network callback. Covered by Task 2 test `stop unregisters the callback`.
4. **`StatFs` throws.** Some devices throw `IllegalArgumentException` for the data directory. The sample must still go out without the two storage fields. Covered by Task 1 test `storage failure omits storage fields only`.
5. **OS release is blank.** `Build.VERSION.RELEASE` can be empty on custom ROMs. Omit `os_version`, do not send an empty string. Covered by Task 3 test `blank os version is omitted`.

---

### Task 1: Memory and storage readings in `DeviceProbe`

**Files:**
- Modify: `SDK/perf/DeviceProbe.kt`
- Modify: `TEST/perf/DeviceProbeTest.kt` (Robolectric)
- Modify: `TEST/testutil/FakeDeviceProbe.kt` (or the existing fake; add the new members)

**Interfaces:**
- Produces, on `internal interface DeviceProbe`: `fun ramFreeMb(): Int?`, `fun storageFreeMb(): Int?`, `fun storageTotalMb(): Int?`, `fun osVersion(): String?`.
- Consumes: the existing `AndroidDeviceProbe(context: Context) : DeviceProbe`.

- [ ] **Step 1: Write the failing tests** in `DeviceProbeTest`
  - `ram free is availMem in MB`: set `ActivityManager.MemoryInfo.availMem = 2_310L * 1_048_576` through `ShadowActivityManager.setMemoryInfo`, then `assertEquals(2310, probe.ramFreeMb())`.
  - `storage reads the data volume`: register `ShadowStatFs` for `Environment.getDataDirectory().path` with block size 4096, 25_000_000 available blocks, 31_250_000 total blocks, then `assertEquals(97_656, probe.storageFreeMb())` and `assertEquals(122_070, probe.storageTotalMb())`.
  - `storage failure omits storage fields only`: make the `StatFs` access throw (wrap the factory in a test seam, see Step 3), then assert `storageFreeMb()` and `storageTotalMb()` are `null` while `ramFreeMb()` is still non-null.

- [ ] **Step 2: Run** `./gradlew :artracker-sdk:testDebugUnitTest --tests '*DeviceProbeTest'`. Expected: FAIL (members not defined).

- [ ] **Step 3: Implement** the four members in `AndroidDeviceProbe`. `ramFreeMb` and `storageFreeMb` wrap their reads in `runCatching { … }.getOrNull()`. Give the class an internal constructor parameter `statFs: (String) -> StatFs = { StatFs(it) }` so the failure test can inject a throwing factory. `osVersion()` is added in Task 3; declare it now returning `Build.VERSION.RELEASE`.

- [ ] **Step 4: Run** the same command. Expected: PASS.

- [ ] **Step 5: Commit** `feat(perf): free RAM and storage readings in DeviceProbe`.

---

### Task 2: `NetworkProbe` with a cached default-network callback

**Files:**
- Create: `SDK/perf/NetworkProbe.kt`
- Test: `TEST/perf/NetworkProbeTest.kt` (Robolectric, `ShadowConnectivityManager`)

**Interfaces:**
- Produces:
  - `internal interface NetworkProbe { fun start(); fun stop(); fun current(): String? }`. `current()` returns one of `"wifi"`, `"cellular"`, `"ethernet"`, `"none"`, or `null` when the type is unknown.
  - `internal class ConnectivityNetworkProbe(context: Context) : NetworkProbe`. `start()` calls `ConnectivityManager.registerDefaultNetworkCallback` once (idempotent) and caches the latest `NetworkCapabilities` in a `@Volatile` field. `onLost` sets the cache to `"none"`. `stop()` unregisters (idempotent).
  - `internal fun networkKind(caps: NetworkCapabilities?): String?` (pure): `null` caps gives `"none"`; transport order `WIFI`, `CELLULAR`, `ETHERNET`; anything else gives `null`.
- Consumes: nothing from earlier tasks.

- [ ] **Step 1: Write the failing tests** in `NetworkProbeTest`
  - `networkKind maps transports`: wifi caps gives `"wifi"`, cellular gives `"cellular"`, ethernet gives `"ethernet"`.
  - `unknown transport omits net`: caps with only `TRANSPORT_VPN` gives `null`.
  - `no active network reports none`: after `start()` with the shadow default network cleared, `current()` is `"none"`.
  - `callback updates the cache`: simulate `onCapabilitiesChanged` with cellular caps, then `current()` is `"cellular"`.
  - `stop unregisters the callback`: after `stop()`, `ShadowConnectivityManager.getNetworkCallbacks()` is empty; calling `stop()` twice does not throw.

- [ ] **Step 2: Run** `./gradlew :artracker-sdk:testDebugUnitTest --tests '*NetworkProbeTest'`. Expected: FAIL.

- [ ] **Step 3: Implement** the types above. Seed the cache in `start()` from `cm.activeNetwork` so the first sample does not wait for a callback. Guard every call with `runCatching`.

- [ ] **Step 4: Run** the same command. Expected: PASS.

- [ ] **Step 5: Commit** `feat(perf): NetworkProbe caching the default network type`.

---

### Task 3: Wire fields into `PerfSample`, the `device` block and the monitor lifecycle

**Files:**
- Modify: `SDK/perf/PerfSample.kt`, `SDK/perf/PerfSampler.kt`, `SDK/perf/PerfMonitor.kt`
- Modify: `TEST/perf/PerfSamplerTest.kt`, `TEST/perf/PerfMonitorTest.kt`
- Modify: `TEST/perf/PerfCoreIntegrationTest.kt` (golden attributes)

**Interfaces:**
- Consumes: `DeviceProbe.ramFreeMb()`, `.storageFreeMb()`, `.storageTotalMb()`, `.osVersion()` (Task 1) and `NetworkProbe.current()/start()/stop()` (Task 2).
- Produces:
  - `PerfSample` gains `ramFreeMb: Int? = null`, `storageFreeMb: Int? = null`, `net: String? = null`. `toWire()` writes `ram_free_mb`, `storage_free_mb`, `net` and omits nulls.
  - The `device` map built by `PerfMonitor` gains `os_version` (omitted when null or blank, truncated to 32 chars) and `storage_mb` (omitted when null), next to the existing `ram_mb`, `cores`, `refresh_hz`.
  - `PerfMonitor` takes a `NetworkProbe` and calls `start()` where it starts sampling and `stop()` where it stops.

- [ ] **Step 1: Write the failing tests**
  - `PerfSamplerTest.sample carries ram storage and net`: fake probes return `ramFreeMb = 2310`, `storageFreeMb = 51200`, `net = "wifi"`; the produced sample has those values and `toWire()` contains `"ram_free_mb": 2310`, `"storage_free_mb": 51200`, `"net": "wifi"`.
  - `PerfSamplerTest.null readings are omitted`: probes return `null`; `toWire()` has none of the three keys.
  - `PerfMonitorTest.device block has os version and storage`: the emitted item's `attributes.device` equals `{ram_mb, storage_mb: 128000, os_version: "14", cores, refresh_hz}` for the fake probe values.
  - `PerfMonitorTest.blank os version is omitted`: `osVersion()` returns `"  "`; `device` has no `os_version` key.
  - `PerfMonitorTest.stop stops the network probe`: calling the monitor's stop path calls `NetworkProbe.stop()` exactly once; starting calls `start()` exactly once.

- [ ] **Step 2: Run** `./gradlew :artracker-sdk:testDebugUnitTest --tests '*PerfSamplerTest' --tests '*PerfMonitorTest'`. Expected: FAIL.

- [ ] **Step 3: Implement** the fields, the `toWire()` additions and the `device` additions. `PerfSampler` reads the three values once per tick through the probes.

- [ ] **Step 4: Run** the full check from Global Constraints. Expected: all green, and `PerfCoreIntegrationTest` golden JSON updated to include the new keys.

- [ ] **Step 5: Verify against the backend contract.** Print one item's `attributes` JSON from `PerfCoreIntegrationTest` and confirm every new key and the `net` values match the Global Constraints exactly (the backend drops anything else without an error).

- [ ] **Step 6: Commit** `feat(perf): send os version, RAM, storage and network type`.

---

### Task 4: Docs and demo

**Files:**
- Modify: `README.md` ("Performance monitoring" section) and `PERF.md`
- Modify: the `app/` sample only if it prints perf attributes

- [ ] **Step 1:** Document the five new fields, that they come from `perf` and not `session.start`, and that `net` needs no extra permission.
- [ ] **Step 2:** Run the full check from Global Constraints. Expected: PASS.
- [ ] **Step 3:** Run the demo app on a device with performance enabled, open the dashboard replay for that session, and confirm the Performance table shows OS, RAM, Storage and Network rows (see the artracker web plan note below).
- [ ] **Step 4: Commit** `docs(perf): device context fields`.

## Dashboard check (outside this repo)

The dashboard side already ships in `artracker` (`PerformancePanel.tsx`, backend `ingest/perf.go`). It shows the new rows only after that backend build is deployed. If the table stays empty after Task 4 Step 3, query `session_perf_batches` for the session and check whether `device`/`samples` contain the new keys: present means a frontend problem, absent means the deployed backend is older than the whitelist change or the SDK sent different names.
