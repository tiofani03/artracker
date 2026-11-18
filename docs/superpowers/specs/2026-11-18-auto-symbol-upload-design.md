# Automatic symbol upload (Android mapping, iOS dSYM) — design

## Intent

Crash stack traces from release builds are obfuscated (`a.b.c()`). Artracker can already translate them back,
but only if the build's "dictionary" (Android `mapping.txt`, iOS dSYM) was uploaded, and today the upload needs a
dashboard admin login. Nobody can do that from a build machine, so it is done by hand or forgotten.

Goal: the same experience as Firebase Crashlytics. A developer installs a Gradle plugin (Android) or adds one Run
Script line (iOS) once, and every release build uploads its symbols by itself.

## Goals

- Upload endpoints that accept the project's secret key (`art_sec_...`) and need no project id: the key decides
  the project.
- Android: a Gradle plugin that uploads `mapping.txt` after every minified build, keyed by the app's version name.
- iOS: a script shipped with the SDK, called from a Run Script phase, that uploads the app target's dSYM.
- Real-size files work: mappings of tens of MB, dSYMs of a few hundred MB.
- A failed or skipped upload never breaks the developer's build by default.

## Non-goals

- Web source map tooling (the backend route is opened too, but no bundler plugin).
- Matching symbols by build UUID. Artracker still matches by release version string; the newest upload for a
  version wins.
- dSYMs of app extensions and embedded frameworks (the lookup uses one dSYM per version).
- Moving artifact storage from Postgres `BYTEA` to R2.

## Anti-goals

- A secret key of project A can write symbols into project B.
- A public (`art_live_`) or MCP key can upload symbols.
- Raising the body limit lets every endpoint (ingest, dashboard) accept 256 MB requests.
- A developer's local build fails because no key is configured.

## Constraints (found in the code)

- Upload handlers already exist (`backend/internal/handler/artifacts.go`, `UploadSourceMap` in `issues.go`) and
  read the project from `c.Locals("projectID")`. They are wired behind JWT + Admin only (`routes.go:107-109`).
- Fiber runs with the default 4 MiB body limit and a 10 s read timeout (`cmd/api/main.go`); nginx caps `/api/` at
  2 MB (`frontend/nginx.conf`). Real symbol files do not fit.
- Symbolication happens when an issue is viewed (`loadProguardRetracer` / `loadIOSSymbolicator` in `issues.go`),
  matching `project_release_artifacts.version` to the event's `app_version`. So the plugin must use exactly the
  version string the SDK reports, and a late upload still fixes older crashes.
- The Android and iOS SDKs live in separate repos (`artracker-android`, `artracker-ios`) not available here.

## Approaches

- **A. Gradle plugin + Xcode script that upload directly (chosen).** Closest to Crashlytics; the upload is one
  HTTP request, so each side stays small. Cost: upload logic exists twice (Kotlin, shell).
- B. One `artracker-cli` binary called by both. One implementation, but every build machine must install it, which
  defeats "simple".
- C. Backend only, with copy-paste snippets in the docs. Fastest, but not "install once and forget".

## Design

### Backend (this repo)

- New key-authenticated routes, project taken from the key:
  - `POST /api/v1/sdk/releases/:version/proguard`
  - `POST /api/v1/sdk/releases/:version/dsyms`
  - `POST /api/v1/sdk/releases/:version/sourcemaps`
  Header `X-Artracker-Key: art_sec_...`; query `build` and `file` as today. Middleware `RequireSecretKey` reads
  `api_keys` directly (not the ingest cache) so a revoked key stops at once. Missing/unknown/revoked/MCP key → 401;
  public key → 403 `secret key required`.
- The existing JWT routes stay as they are.
- `Content-Encoding: gzip` bodies are decompressed (cap 1 GiB decompressed).
- Re-uploading identical content for the same project, version, type and file returns `200 {"status":"unchanged"}`
  instead of storing another copy.
- Body size: `ARTIFACT_MAX_BYTES` (default 256 MiB) applies only to `/api/v1/sdk/releases/*`; every other route
  keeps the 4 MiB cap. nginx gets a matching location block. Insert timeouts become 60 s.

### Android (`artracker-android`)

```kotlin
plugins { id("<sdk-group>.symbols") }
artracker {
    apiUrl = "https://art.example.com"   // or env ARTRACKER_API_URL
    // secret key: env ARTRACKER_SECRET_KEY or Gradle property artracker.secretKey
}
```
For every variant with minification, the plugin registers `uploadArtracker<Variant>Mapping`, run after
`assemble<Variant>` and `bundle<Variant>`. It gzips the mapping and posts it with `version` = the variant's
`versionName` and `build` = `versionCode`. No key → warning and skip. Upload error → warning, unless
`failOnUploadError = true`.

### iOS (`artracker-ios`)

Script `Scripts/artracker-upload-symbols`, distributed with the SDK (SPM checkout and CocoaPods). Run Script phase:
```
"${BUILD_DIR%/Build/*}/SourcePackages/checkouts/artracker-ios/Scripts/artracker-upload-symbols"
```
Input file (needed with user script sandboxing):
`${DWARF_DSYM_FOLDER_PATH}/${DWARF_DSYM_FILE_NAME}/Contents/Resources/DWARF/${TARGET_NAME}`.
It reads `ARTRACKER_API_URL` and `ARTRACKER_SECRET_KEY` from the environment / build settings, takes the version
from the built Info.plist (`CFBundleShortVersionString`, build `CFBundleVersion`), zips the target's dSYM and posts
it. Skips when no dSYM is produced or no key is set; never fails the build unless `ARTRACKER_FAIL_ON_ERROR=1`.

## Left to the builder

- The Gradle plugin id and Maven coordinates follow the SDK's existing group.
- The CocoaPods script path follows the pod's name.
- Confirm in each SDK which string it sends as `app_version`; the plugin/script must send the same.
