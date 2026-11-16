# Plan: Login Google yang kembali ke app (Android & Ubuntu)

Status: draft, 2026-11-16. Lanjutan dari [plan-app-tauri.md](plan-app-tauri.md).

## Masalah

Di Android dan Ubuntu, "Continue with Google" membuka browser, login berhasil, tetapi browser berakhir
di dashboard **web** dan tidak kembali ke app.

## Penyebab (sudah diverifikasi)

Server produksi `artracker.exlm.my.id` masih menjalankan kode lama. Pengecekan 2026-11-16:

| Cek | Hasil | Artinya |
|---|---|---|
| `GET /api/v1/auth/google/login?client=app&code_challenge=...` | hanya menyetel cookie `oauth_state`; **tidak ada** `oauth_app_challenge` | parameter `client=app` diabaikan, jadi dijalankan alur web biasa |
| `POST /api/v1/auth/app/exchange` | **404** | endpoint penukar kode belum ada |

Jadi callback ke app tidak pernah dibuat. Ini bukan bug di app. Di mesin lokal, alur lengkapnya sudah lolos
tes backend (`internal/handler/auth_app_test.go`): login, callback, bounce page `artracker://`, exchange
dengan PKCE, refresh dan logout lewat body.

Login Google tidak bisa dilakukan di dalam webview app (Google menolak dengan `disallowed_useragent`),
jadi login **tetap di browser**. Yang harus berfungsi adalah **kembali ke app** setelahnya.

## Alur target

```
App ─ buka browser ─► /auth/google/login?client=app&code_challenge=C
                       │  cookie: oauth_state + oauth_app_challenge=C
                       ▼
                    Google ─► /auth/callback/google
                       │  simpan kode sekali pakai (hash, TTL 60 dtk, terikat C)
                       ▼
                    halaman "Returning to ARTracker…" ─► artracker://auth/callback?code=X
                       ▼
App ◄─ deep link ───── OS membuka / mengaktifkan app
App ─ POST /auth/app/exchange {code: X, code_verifier: V} ─► access_token + refresh_token
```

## Fase A: Deploy backend (prasyarat untuk semuanya)

Deploy berjalan otomatis: setiap push ke `main` membangun image `api` dan `web`, lalu SSH ke VPS dan
`docker compose -f docker-compose.prod.yml up -d` (`.github/workflows/docker-publish.yml`). Migrasi
`0015_app_auth_codes` jalan sendiri saat API boot dan hanya menambah tabel baru, jadi login web tidak terpengaruh.

| # | Tugas | Siapa |
|---|---|---|
| A1 | Commit perubahan (backend, frontend, `src-tauri`, `.env.app`, migrasi, tes, docs) ke branch dan buka PR | saya, atas persetujuanmu |
| A2 | Review dan merge PR ke `main` (memicu build + deploy) | kamu |
| A3 | Tambahkan ke `.env` di VPS: `CORS_ORIGINS=<yang sudah ada>,tauri://localhost,http://tauri.localhost`, lalu `docker compose -f docker-compose.prod.yml up -d` | kamu (butuh akses VPS) |
| A4 | Verifikasi server (perintah di bawah) | saya |

Verifikasi A4 (harus lolos sebelum lanjut):

```bash
C=$(python3 -c "print('A'*43)")
# 1. harus ada cookie oauth_app_challenge
curl -s -D - -o /dev/null "https://artracker.exlm.my.id/api/v1/auth/google/login?client=app&code_challenge=$C" | grep -i set-cookie
# 2. harus 401, bukan 404
curl -s -o /dev/null -w "%{http_code}\n" -X POST -H 'Content-Type: application/json' -d '{}' \
  https://artracker.exlm.my.id/api/v1/auth/app/exchange
# 3. preflight dari origin app harus mengembalikan access-control-allow-origin
curl -s -D - -o /dev/null -X OPTIONS -H 'Origin: tauri://localhost' -H 'Access-Control-Request-Method: GET' \
  -H 'Access-Control-Request-Headers: authorization' https://artracker.exlm.my.id/api/v1/auth/me | grep -i access-control
```

Rollback: kembalikan image `api` ke tag SHA sebelumnya. Tabel baru tidak mengganggu kode lama.

## Fase B: Ubuntu

Deep link Ubuntu sudah terbukti jalan lewat `xdg-open` (instance tunggal menerima URL). Yang belum: alur nyata
dari Google.

| # | Tugas | Siapa |
|---|---|---|
| B1 | Jalankan app (`.deb` terpasang, atau binary rilis), klik Continue with Google, pilih akun | kamu (butuh akunmu) |
| B2 | Pastikan browser menampilkan "Returning to ARTracker…", klik **Open ARTracker** bila diminta, dan jendela app yang **sama** masuk ke dashboard | kamu |
| B3 | Jika gagal, ambil `~/.local/share/applications/artracker-handler.desktop`, `xdg-mime query default x-scheme-handler/artracker`, dan log app; saya telusuri | saya |
| B4 | Uji sesi tersimpan: tutup app, buka lagi, harus tetap login (refresh token di store) | kamu |
| B5 | Uji logout lalu login ulang | kamu |

Catatan teknis Ubuntu:
- `.deb` mendaftarkan skema `artracker://` lewat `.desktop` saat dipasang. Untuk AppImage atau binary
  mentah, `register_all()` di `lib.rs` menulis `artracker-handler.desktop` ke `~/.local/share/applications`.
- Browser (Firefox/Chrome) bisa menampilkan dialog "Open with ARTracker?" saat pertama. Itu normal; centang
  "selalu".
- Di sesi Wayland, fokus jendela yang sudah terbuka bisa ditolak compositor. Jika jendela tidak naik ke depan,
  itu perilaku compositor, bukan kegagalan login.

## Fase C: Android

| # | Tugas | Siapa |
|---|---|---|
| C1 | Pasang APK debug baru (jika kode app berubah) dan klik Continue with Google | kamu / saya via `adb` |
| C2 | Setelah login, Chrome harus membuka app lewat `artracker://`; jika Chrome hanya menampilkan halaman bounce, ketuk **Open ARTracker** | kamu |
| C3 | Verifikasi: app masuk dashboard, dan tab Chrome tidak menggantung | kamu |
| C4 | Uji kasus app dimatikan saat login berlangsung (verifier tersimpan di store, jadi exchange tetap berhasil) | saya via `adb force-stop` + kamu |

Risiko Android:
- Chrome bisa memblokir redirect otomatis ke skema kustom tanpa gerakan pengguna. Itu sebabnya halaman bounce
  punya tombol **Open ARTracker**. Jika di perangkatmu tombol ini terlalu sering dibutuhkan, lanjut ke Fase D2.
- Beberapa HP (termasuk Infinix) punya penghemat baterai yang mematikan app di latar. Bila app terbunuh saat
  login, verifier sudah di store sehingga tetap aman (C4).

## Fase D: Perbaikan pengalaman (setelah A–C lolos)

| # | Perbaikan | Platform | Catatan |
|---|---|---|---|
| D1 | Status "Menunggu login di browser…" + tombol Batal + timeout 2 menit di halaman Login app | keduanya | Sekarang setelah klik, app tidak memberi tanda apa pun |
| D2 | **Android App Links** (`https://artracker.exlm.my.id/app/callback` + `assetlinks.json`) sebagai ganti skema kustom | Android | Redirect tidak diblokir Chrome dan tanpa dialog. Butuh SHA-256 keystore rilis, jadi dikerjakan setelah keystore ada |
| D3 | **Custom Tabs** untuk Android: login muncul sebagai tab di atas app, bukan Chrome penuh | Android | Perlu plugin kecil; alur backend sama |
| D4 | Rapikan halaman bounce: teks lebih jelas, tombol "Tutup tab", tema gelap | keduanya | Hanya HTML di `appBounce` (`auth.go`) |
| D5 | Login Google native (Credential Manager) tanpa browser | Android | Pekerjaan besar; tunda sampai ada Play Store |

Urutan yang saya sarankan: A → B → C, lalu D1 dan D4 (murah), D3 jika Android masih terasa berat, D2 saat keystore rilis siap.

## Pembagian tugas

**Bagianmu**
- [ ] Setujui commit dan PR (A1), lalu merge ke `main` (A2)
- [ ] Update `CORS_ORIGINS` di `.env` VPS dan restart stack (A3)
- [ ] Login dengan akun Google sungguhan di Ubuntu (B) dan di HP (C); saya tidak memilih akun
- [ ] Kabari hasil tiap langkah; bila gagal, tempel pesan atau screenshot

**Bagianku**
- [ ] Menyiapkan commit dan PR (setelah kamu setuju)
- [ ] Menjalankan verifikasi server A4 dan melaporkan hasilnya
- [ ] Menelusuri kegagalan B dan C dengan log (`adb logcat`, log app Linux, `xdg-mime`)
- [ ] Mengerjakan Fase D yang dipilih

## Kriteria selesai

- [ ] Tiga cek A4 lolos
- [ ] Ubuntu: login Google dari app berakhir di dashboard app, sesi bertahan setelah restart
- [ ] Android: sama, termasuk saat app sempat dimatikan di tengah login
- [ ] Login web biasa (`artracker.exlm.my.id`) tetap berfungsi seperti sebelumnya
