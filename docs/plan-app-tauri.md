# Plan: ARTracker App (Tauri v2) untuk Android & Desktop Linux

Status: 2026-11-16. Fase 0-3 dikerjakan. APK debug jalan di Infinix X6882 (arm64); build Linux
(`.deb` 4.7 MB, `.AppImage` 81 MB) sudah jadi dan jalan di Ubuntu 24.04. Login Google penuh belum
terverifikasi (butuh backend baru ter-deploy).

## Keputusan yang sudah diambil

| Hal | Keputusan |
|---|---|
| URL server | `https://artracker.exlm.my.id` (API: `https://artracker.exlm.my.id/api/v1`) |
| Identifier / package | `id.art.tracker` |
| Nama app | `artracker` |
| Ikon | Sama dengan web: `frontend/public/favicon.svg` (di-generate ke semua ukuran dengan `tauri icon`) |
| Distribusi Android | APK langsung dulu (belum Play Store, jadi `.aab` dan Play Console ditunda) |
| Skema deep link | `artracker://` |
| Mode demo di app | Tidak ada. Build app tidak mengirim `X-Demo-Mode` dan tidak menampilkan entry demo |
| Metode login | Hanya Google (fase ini) |

## Tujuan

Membungkus dashboard ARTracker yang sudah ada (React 19 + Vite) menjadi aplikasi:

- **Desktop Linux** (`.deb` dan `.AppImage`)
- **Android** (`.apk` untuk uji, `.aab` kalau ke Play Store)

Lingkup app: mulai dari halaman **Login** dan seterusnya (dashboard, issues, replay, settings, alerts,
docs di dalam dashboard). **Landing page tidak ikut** ke app.

Di luar lingkup (fase ini): Windows, macOS, iOS, mode offline, backend Go yang dibundel di dalam app.

## Arsitektur

```
┌──────────────────────────────┐         HTTPS          ┌──────────────────────┐
│ App Tauri (Linux / Android)  │ ─────────────────────► │ Server ARTracker     │
│  WebView: build SPA "app"    │   Authorization:Bearer │  Go API + Postgres   │
│  Rust: deep link, keyring,   │                        │  (yang sudah jalan)  │
│        buka browser sistem   │ ◄───── artracker:// ── │  OAuth Google        │
└──────────────────────────────┘     (deep link login)  └──────────────────────┘
```

- App hanya **klien**. Backend tetap satu, di server yang sudah ada.
- Frontend dipakai ulang 1:1. Tidak ada fork kode UI; perbedaan web vs app diatur lewat
  build mode `app` (`vite build --mode app`).
- Lokasi kode Tauri: `frontend/src-tauri/` (konvensi standar Tauri).

## Temuan dari kode saat ini (yang harus diubah)

| # | Temuan | Lokasi | Dampak di app |
|---|---|---|---|
| 1 | Route `/` merender landing page | `frontend/src/main.tsx` | Harus redirect ke `/login` / `/dashboard` di build app |
| 2 | API dipanggil relatif `/api/v1` via proxy Vite | `frontend/src/api.ts` (`BASE`) | Di app tidak ada proxy; isi `VITE_API_BASE` dengan URL server |
| 3 | Login Google redirect balik ke `PUBLIC_URL/auth/callback/google`, lalu ke `PUBLIC_URL/dashboard` | `backend/internal/handler/auth.go` | WebView app tidak bisa menerima hasil login ini; perlu alur deep link |
| 4 | Refresh token hanya lewat cookie `refresh_token` (`SameSite=Lax`, `HttpOnly`) | `auth.go`, `api.ts` (`credentials: 'include'`) | Lintas origin (`tauri://localhost` → server) cookie tidak terkirim; perlu refresh token via body/header |
| 5 | CORS utama: origin dari `CORS_ORIGINS`, `AllowHeaders` tanpa `X-Demo-Mode`; `api.ts` mengirim `X-Demo-Mode` saat belum ada token | `backend/cmd/api/main.go`, `frontend/src/api.ts` | Origin app harus ditambahkan; build app tidak boleh mengirim `X-Demo-Mode` (mode demo tidak dipakai di app), kalau tidak preflight gagal |
| 6 | CORS `/api/v1/replay` dan `/api/v1/sdk`: `AllowHeaders` tanpa `Authorization` | `backend/cmd/api/main.go` | Player replay di app gagal preflight saat memanggil endpoint replay dengan Bearer token |
| 7 | Link eksternal (GitHub, docs) dibuka di tab/jendela yang sama | beberapa komponen | Di app harus dibuka lewat browser sistem |

Origin WebView Tauri v2: Linux `tauri://localhost`, Android `http://tauri.localhost`.

## Desain login (bagian paling penting)

Pakai **browser sistem + deep link + PKCE** (pola standar aplikasi native, RFC 8252). Google Cloud
Console **tidak perlu diubah**, karena redirect Google tetap ke callback backend yang sudah ada.

1. App membuat `code_verifier` acak, lalu membuka browser sistem ke
   `GET /api/v1/auth/google/login?client=app&code_challenge=<S256>`.
2. Backend menyimpan `client=app` + `code_challenge` bersama `state` (cookie state tetap di browser,
   jadi alur CSRF lama tidak berubah).
3. Google → callback backend seperti biasa. Kalau state bertanda `app`, backend **tidak** set cookie,
   tapi membuat **kode tukar sekali pakai** (TTL 60 detik, disimpan hash), lalu redirect ke
   `artracker://auth/callback?code=<kode>`.
4. OS membuka app lewat deep link. App memanggil
   `POST /api/v1/auth/app/exchange { code, code_verifier }` → `{ access_token, refresh_token }`.
5. `refresh_token` disimpan di **keyring OS** (Linux: Secret Service; Android: Keystore), bukan
   `localStorage`.
6. Refresh: `POST /api/v1/auth/refresh` menerima refresh token di body (`{ refresh_token }`) selain
   cookie; respons app menyertakan `refresh_token` baru (rotasi tetap sama seperti sekarang).
   Logout: `POST /auth/logout` juga menerima token di body.

PKCE mencegah app lain yang mendaftarkan skema `artracker://` yang sama mencuri sesi.

## Fase & tugas

### Fase 0: Persiapan lingkungan

- [ ] Pasang dependensi sistem Tauri di Ubuntu 24.04 *(bagianmu, butuh sudo)*
- [ ] `rustup target add aarch64-linux-android armv7-linux-androideabi x86_64-linux-android i686-linux-android`
- [ ] Set `NDK_HOME` ke NDK yang sudah ada (`~/Android/Sdk/ndk/28.2.13676358`)
- [ ] `npm i -D @tauri-apps/cli@^2` dan `npm i @tauri-apps/api@^2` di `frontend/`

### Fase 1: Shell desktop Linux (tanpa login baru)

- [ ] `npx tauri init` → `frontend/src-tauri/` (identifier, nama app, ikon)
- [ ] Build mode `app`: file `frontend/.env.app` berisi `VITE_API_BASE=https://artracker.exlm.my.id/api/v1`
      dan `VITE_TARGET=app`
- [ ] `main.tsx`: kalau `VITE_TARGET=app`, route `/` → redirect ke `/login` (atau `/dashboard` kalau
      sudah login), dan `App` (landing) tidak di-import supaya tidak ikut ke bundle
- [ ] Build app: matikan mode demo (`api.ts` tidak mengirim `X-Demo-Mode`, link/tombol demo
      disembunyikan, `?demo=1` diabaikan)
- [ ] Backend: tambahkan `Authorization` ke `AllowHeaders` CORS `/api/v1/replay` dan `/api/v1/sdk`
- [ ] `CORS_ORIGINS` di server ditambah `tauri://localhost,http://tauri.localhost`
- [ ] Plugin `opener`: semua link eksternal dibuka di browser sistem
- [ ] Uji: `npm run tauri dev`, app terbuka di halaman Login, tidak ada error CORS di konsol

Hasil: app jalan di Linux dan terbuka di halaman Login, belum bisa login. Karena tidak ada mode
demo, uji dashboard, issues, replay player, dan alerts dilakukan setelah Fase 2.

### Fase 2: Login via deep link

- [ ] Backend: `client=app` + `code_challenge` di `OAuthLogin`, simpan di state
- [ ] Backend: cabang app di `OAuthCallback` → kode tukar sekali pakai → redirect `artracker://`
- [ ] Backend: migrasi tabel `app_auth_codes` (hash, user_id, challenge, expires_at, used_at)
- [ ] Backend: `POST /auth/app/exchange`; `/auth/refresh` dan `/auth/logout` terima token di body
- [ ] Backend: tes handler untuk alur app (kode kadaluarsa, dipakai ulang, verifier salah)
- [ ] Tauri: plugin `deep-link` (skema `artracker`), plugin `single-instance` (Linux: deep link
      dikirim ke instance yang sudah jalan)
- [ ] Tauri: penyimpanan refresh token di keyring (command Rust kecil memakai crate `keyring`)
- [ ] Frontend: `api.ts` & `AuthContext` punya jalur app (refresh via body, token dari keyring);
      `LoginPage` di app membuka browser sistem, bukan link biasa
- [ ] Uji end-to-end login, refresh setelah app ditutup-buka, logout
- [ ] Uji fitur dengan akun asli: dashboard, issues, replay player, alerts, settings

### Fase 3: Android

- [ ] `npx tauri android init`
- [ ] Deep link Android (intent filter skema `artracker`) lewat konfigurasi plugin `deep-link`
- [ ] Keyring Android (Keystore) untuk refresh token
- [ ] Cek tampilan dashboard di layar HP: sidebar, tabel, replay player, modal. Perbaiki CSS yang
      pecah (tanpa mengubah tampilan web desktop)
- [ ] Tombol back Android: navigasi riwayat, bukan menutup app
- [ ] Uji di emulator dan minimal 1 HP fisik
- [ ] Build `.apk` rilis yang ditandatangani keystore rilis, dibagikan langsung (`.aab` ditunda
      sampai masuk Play Store)

### Fase 4: Rilis & distribusi

- [ ] Ikon & splash final (Linux + Android adaptive icon)
- [ ] Workflow GitHub Actions `app-release.yml`: build `.deb`, `.AppImage`, `.apk`/`.aab` saat tag
- [ ] Auto-update Linux (plugin `updater`, kunci signing updater)
- [ ] Versi app mengikuti versi rilis server (catat versi API minimum yang didukung)
- [ ] Catatan rilis & dokumentasi instalasi di README

## Bagianmu (yang tidak bisa / tidak boleh saya putuskan sendiri)

### Keputusan (tolong jawab sebelum Fase 1)

- [x] **URL server**: `https://artracker.exlm.my.id`
- [x] **Identifier app**: `id.art.tracker` (tidak bisa diganti setelah APK dibagikan, karena update
      butuh identifier dan keystore yang sama)
- [x] **Nama app**: `artracker`; **ikon**: sama dengan web
- [x] **Distribusi Android**: APK langsung dulu
- [x] **Skema deep link**: `artracker://`
- [x] **Mode demo**: tidak ikut di app
- [x] **Login**: hanya Google dulu

### Pekerjaan manual

- [ ] Pasang dependensi sistem (butuh sudo):
      ```bash
      sudo apt install libwebkit2gtk-4.1-dev build-essential curl wget file libxdo-dev \
        libssl-dev libayatana-appindicator3-dev librsvg2-dev
      ```
- [ ] Update `CORS_ORIGINS` di `.env` server dan deploy backend setelah Fase 1 & 2 di-merge
- [ ] Buat **keystore rilis Android** dan simpan aman. Kalau hilang, APK baru tidak bisa
      menimpa APK lama di HP user (harus uninstall dulu). Masukkan ke GitHub Secrets kalau build lewat CI
- [ ] Di HP penerima APK: izinkan "Install unknown apps" untuk aplikasi yang dipakai membuka APK
- [ ] (Nanti) Akun **Google Play Console** kalau pindah ke Play Store
- [ ] Buat **kunci signing updater** Tauri dan simpan di GitHub Secrets (Fase 4)
- [ ] Uji manual di perangkat: login, replay, notifikasi alert, di HP fisik dan desktop
- [ ] Review & merge PR tiap fase

### Yang saya kerjakan

Semua kode di Fase 1 sampai 4: setup Tauri, perubahan frontend, endpoint & migrasi backend beserta
tesnya, konfigurasi Android, workflow CI, dan dokumentasi. Saya juga menjalankan app di Linux dan
emulator Android untuk verifikasi sebelum menyerahkan tiap fase.

## Risiko

| Risiko | Mitigasi |
|---|---|
| WebKitGTK (Linux) beda perilaku dengan Chromium: canvas replay, CSS `:has()`, font | Uji replay player sejak Fase 1; WebKitGTK 2.44+ (Ubuntu 24.04) sudah mendukung `:has()` |
| Deep link Linux hanya terdaftar saat app terinstal (`.deb`/`.AppImage`), tidak saat `tauri dev` | Pakai `register()` dari plugin deep-link di mode dev |
| Dashboard belum nyaman di layar HP | Fase 3 khusus perbaikan layout mobile; prioritaskan halaman Issues & Alerts |
| Perubahan auth backend memengaruhi login web | Jalur web (cookie) tidak diubah; jalur app hanya aktif dengan `client=app`; ada tes handler |
| Versi app lama vs API baru | Endpoint app diberi versi; app cek versi server saat start |

## Estimasi kasar

| Fase | Perkiraan |
|---|---|
| 0 + 1 Shell Linux | 1–2 hari |
| 2 Login deep link | 2–3 hari |
| 3 Android | 3–5 hari (tergantung perbaikan layout) |
| 4 Rilis & CI | 1–2 hari |

## Status & cara menjalankan

### Sudah ada

- `frontend/src-tauri/`: Tauri v2, identifier `id.art.tracker`, plugin deep-link, opener, store,
  single-instance (desktop). Ikon dari glyph yang sama dengan web (`src-tauri/app-icon.svg`).
- Build mode app (`npm run build:app`, `frontend/.env.app`): tanpa landing page (chunk landing tidak
  ikut bundle), tanpa mode demo, API ke `https://artracker.exlm.my.id/api/v1`, bearer token saja.
- Backend: migrasi `0015_app_auth_codes`, `client=app` + PKCE di login/callback, halaman bounce ke
  `artracker://auth/callback`, `POST /auth/app/exchange`, refresh/logout lewat body. Tes di
  `internal/handler/auth_app_test.go`.
- Refresh token disimpan lewat `plugin-store` (file privat app), **bukan** keyring OS seperti di
  rencana awal: crate `keyring` tidak mendukung Android. Pindah ke keyring di Linux bisa menyusul.
- Terverifikasi di HP: app terbuka di Login, deep link `artracker://auth/callback?error=...`
  menampilkan pesan error, tombol Google membuka Chrome ke accounts.google.com.

### Belum terverifikasi

- **Login sampai tuntas + dashboard**: butuh backend baru ter-deploy ke `artracker.exlm.my.id`
  (migrasi 0015 + endpoint exchange) dan `CORS_ORIGINS` berisi `tauri://localhost,http://tauri.localhost`.
  Server saat ini belum punya keduanya, jadi login dari app belum bisa selesai.
- **Linux**: build dan jalan sudah terverifikasi (lihat di bawah); sisanya sama dengan Android: login penuh menunggu deploy.
- Replay player, alerts, settings di layar HP (menunggu login).
- Catatan koreksi: endpoint `/replay/download-chunk` memakai URL bertanda tangan tanpa header
  `Authorization`, jadi CORS replay **tidak** perlu diubah (berbeda dari draf awal). URL chunk
  di-rewrite ke origin API di build app.

### Cara menjalankan

```bash
# sekali: dependensi Linux (butuh sudo)
sudo apt install libwebkit2gtk-4.1-dev build-essential curl wget file libxdo-dev \
  libssl-dev libayatana-appindicator3-dev librsvg2-dev

cd frontend
npm install

# Linux (dev, API produksi): jendela desktop dengan hot reload memakai Vite
npm run tauri dev
# Linux (paket): src-tauri/target/release/bundle/{deb,appimage}
npm run tauri build

# Android (env: ANDROID_HOME, NDK_HOME, JAVA_HOME=JDK 17)
export ANDROID_HOME=$HOME/Android/Sdk NDK_HOME=$ANDROID_HOME/ndk/28.2.13676358
npm run tauri android build -- --apk --debug --target aarch64    # arm64 (HP)
adb install -r src-tauri/gen/android/app/build/outputs/apk/universal/debug/app-universal-debug.apk
```

APK debug ditandatangani debug key: cukup untuk uji dan dibagikan langsung. Rilis resmi (`--apk`
tanpa `--debug`) butuh keystore rilis milikmu (lihat "Bagianmu").

### Linux: dependensi sistem (perlu `sudo`, sudah dikerjakan)

Status 2026-11-16: kode dan konfigurasi Linux sudah siap, tetapi **belum pernah dibuild atau dijalankan**
karena paket sistem WebKitGTK belum terpasang (`pkg-config webkit2gtk-4.1` gagal).

Percobaan pertama `sudo apt install ...` gagal bukan karena paket Tauri, melainkan karena apt ikut
memproses upgrade kernel `6.8.0-147` yang tertunda: build modul **nvidia-dkms 570** gagal, dpkg berhenti,
dan paket kernel (`linux-image`, `linux-headers`, `linux-generic`) tertinggal setengah terkonfigurasi.
Selama itu belum beres, `apt install` apa pun bisa ikut tersangkut. Kernel yang sedang berjalan tidak rusak.

Langkah (satu baris utuh, jalankan sendiri karena meminta password):

```bash
# 1. tahan paket kernel bermasalah supaya apt tidak mengurusnya, lalu pasang dependensi Tauri
sudo apt-mark hold linux-image-6.8.0-147-generic linux-headers-6.8.0-147-generic linux-headers-generic linux-generic \
  && sudo apt install -y --no-upgrade libwebkit2gtk-4.1-dev build-essential curl wget file libxdo-dev \
     libssl-dev libayatana-appindicator3-dev librsvg2-dev

# 2. cek
pkg-config --modversion webkit2gtk-4.1
```

Kalau `apt install` masih tersangkut di paket kernel, perbaiki dulu masalah nvidia-dkms (cek
`/var/lib/dkms/nvidia/*/build/make.log`), lalu `sudo dpkg --configure -a`. Itu urusan sistem, bukan proyek ini.
Jangan jalankan `apt-mark hold` tanpa nama paket (hasilnya `E: No packages found`).

Setelah paket terpasang:

```bash
cd frontend
npm run tauri dev      # uji cepat: jendela desktop, harus terbuka di halaman Login
npm run tauri build    # menghasilkan .deb dan .AppImage di src-tauri/target/release/bundle/
```

Yang perlu dicek di Linux: jendela terbuka di Login (tanpa landing), tombol Google membuka browser sistem,
dan `artracker://` kembali ke app yang sama (plugin `single-instance` + `register_all()` di `lib.rs`).
Login penuh tetap bergantung pada backend baru yang sudah ter-deploy (lihat "Belum terverifikasi").

### Hasil uji Linux (2026-11-16)

- `npx tauri build --bundles deb appimage` berhasil: `src-tauri/target/release/bundle/deb/artracker_0.1.0_amd64.deb`
  dan `.../appimage/artracker_0.1.0_amd64.AppImage`.
- Binary `src-tauri/target/release/artracker` terbuka di halaman Login (1280x800, tanpa landing).
- `xdg-open "artracker://auth/callback?error=access_denied"` diteruskan ke instance yang sedang berjalan
  (single-instance) dan menampilkan pesan error di Login.
- Di sesi Wayland, jendela tidak terlihat oleh `xwininfo`/`xwd`; untuk capture jalankan dengan `GDK_BACKEND=x11`.
- Efek samping: `register_all()` menulis `~/.local/share/applications/artracker-handler.desktop` agar
  skema `artracker://` terdaftar saat belum ada paket terpasang. Hapus file itu kalau tidak diperlukan.
