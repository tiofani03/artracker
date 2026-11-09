# Rencana Implementasi Perbaikan — Web Dashboard (`artracker/frontend`)

> **Target Repositori**: `artracker` (`frontend/`)  
> **Konteks Audit**: Sesi Android `01a1bf82-03b1-7c23-8acc-75ac4b72cefb` (`ai-mastery-android`, `project_id = 5`)  
> **Dokumen Terkait**: [Audit Utama (`docs/plan.md`)](file:///Users/tiofani/IdeaProjects/artracker/docs/plan.md) · [Plan Android SDK (`docs/plan-android.md`)](file:///Users/tiofani/IdeaProjects/artracker/docs/plan-android.md)  
> **Tujuan**: Memastikan sesi replay Android (baik yang menggunakan format synthetic `rrweb` seperti sesi `01a1bf8…2cefb` yang sudah tersimpan di Cloudflare R2, maupun yang ke depannya menggunakan format `artracker-mobile-v1`) tampil rapi, proporsional di dalam bingkai ponsel Android, memiliki hierarki visual wireframe yang setara dengan iOS, bebas dari teks bertumpuk (*ghosting*), dan sinkron dengan timeline serta daftar Session Queue.

---

## 1. Daftar Masalah di Sisi Web (`frontend/`)

| ID | Masalah di Sisi Web | File Terdampak | Prioritas |
| :--- | :--- | :--- | :---: |
| **WEB-01** | Sesi Android yang mengirim chunk synthetic `rrweb` (`href: "android://..."`, `data-artracker-type`) diklasifikasikan sebagai `model.kind = 'web'`, sehingga mode `Auto` memaksa bingkai **Desktop Browser (`1440 × 900`)** alih-alih bingkai ponsel `.or-phone-frame--android`. | [`frontend/src/replay/normalize.ts`](file:///Users/tiofani/IdeaProjects/artracker/frontend/src/replay/normalize.ts#L157-L186)<br>[`frontend/src/components/ReplayPlayer.tsx`](file:///Users/tiofani/IdeaProjects/artracker/frontend/src/components/ReplayPlayer.tsx#L200-L229) | **P0 (Kritis)** |
| **WEB-02** | DOM sintetis Android dirender di dalam `<iframe>` `rrweb-player` dengan `<head>` kosong (tanpa CSS). Akibatnya teks jatuh ke font default **`16px Times New Roman` (serif)** dan terpotong oleh `overflow: hidden`, sedangkan 29 `BUTTON` dan 183 `CONTAINER` tampil transparan tanpa border/warna. | [`frontend/src/replay/normalize.ts`](file:///Users/tiofani/IdeaProjects/artracker/frontend/src/replay/normalize.ts#L139-L155) | **P0 (Kritis)** |
| **WEB-03** | Pada chunk `FullSnapshot` yang terekam saat animasi transisi Jetpack Compose (`android://lab` dan `android://lab/_id_`), dua container layar utama berada pada koordinat yang sama (`left:0px;top:160px;width:320px;height:432px`) dengan background transparan sehingga teks layar lama dan layar baru saling menimpa selama ~2 detik. | [`frontend/src/replay/normalize.ts`](file:///Users/tiofani/IdeaProjects/artracker/frontend/src/replay/normalize.ts#L139-L155) | **P0 (Kritis)** |
| **WEB-04** | Saat bingkai `.or-phone-frame--android` aktif, kanvas `320 × 716` di-scale menjadi `223 × 500px` di dalam wadah `308 × 502px` berlatar `--color-graphite` gelap, menghasilkan blok hitam (*letterboxing*) selebar `42,5px` di kiri-kanan layar putih `#fafafa`. | [`frontend/src/components/ReplayPlayer.tsx`](file:///Users/tiofani/IdeaProjects/artracker/frontend/src/components/ReplayPlayer.tsx#L223-L229)<br>[`frontend/src/dashboard.css`](file:///Users/tiofani/IdeaProjects/artracker/frontend/src/dashboard.css#L1986-L2006) | **P1 (Mayor)** |
| **WEB-05** | Route strip di atas player (`recordedUrl`) hanya mengambil event `EventType.Meta` pertama (`android://MainActivity`) dan tidak ikut berubah saat pengguna berpindah ke `android://skenario`, `android://lab`, dan `android://lab/_id_`. | [`frontend/src/components/ReplayPlayer.tsx`](file:///Users/tiofani/IdeaProjects/artracker/frontend/src/components/ReplayPlayer.tsx#L258-L281) | **P1 (Mayor)** |
| **WEB-06** | Kalkulasi marker timeline di `ReplayPlayer.tsx` melempar event telemetri yang terjadi sebelum `baseTimestampMs` chunk pertama ke posisi proporsional (`10%`, `20%`) di tengah scrubber. | [`frontend/src/components/ReplayPlayer.tsx`](file:///Users/tiofani/IdeaProjects/artracker/frontend/src/components/ReplayPlayer.tsx#L302-L333) | **P2 (Menengah)** |
| **WEB-07** | Indikator `.dash-session-item__dot--clean` di Session Queue memakai variabel CSS `--color-active` yang tidak ada di `tokens.css`, badge platform `.dash-session-item__platform` duplikat & transparan di light mode, serta chunk iOS lama tanpa field `format: "artracker-mobile-v1"` ditolak oleh `extractMobileFrames()`. | [`frontend/src/dashboard.css`](file:///Users/tiofani/IdeaProjects/artracker/frontend/src/dashboard.css#L1265-L1291)<br>[`frontend/src/replay/normalize.ts`](file:///Users/tiofani/IdeaProjects/artracker/frontend/src/replay/normalize.ts#L100-L127) | **P2 (Menengah)** |

---

## 2. Langkah Implementasi Detail per File

### Tugas 1: Deteksi Sesi Mobile `rrweb` & Injeksi Wireframe Stylesheet + Anti-Ghosting (`frontend/src/replay/normalize.ts`)

**File**: [`frontend/src/replay/normalize.ts`](file:///Users/tiofani/IdeaProjects/artracker/frontend/src/replay/normalize.ts)

1. **Perluas Tipe `ReplayModel` (`normalize.ts:28-38`)**:
   Tambahkan metadata opsional pada cabang `kind: 'web'` agar `ReplayPlayer` mengetahui apakah stream `rrweb` tersebut berasal dari aplikasi mobile (`android` / `ios`) beserta dimensi kanvas aslinya:
   ```ts
   export type ReplayModel =
     | {
         kind: 'web'
         events: eventWithTime[]
         startMs: number
         endMs: number
         inferredPlatform?: 'android' | 'ios' | 'web'
         viewport?: { width: number; height: number }
       }
     | {
         kind: 'mobile'
         platform: 'ios' | 'android'
         frames: MobileFrame[]
         touches: MobileTouch[]
         startMs: number
         endMs: number
       }
     | { kind: 'empty' }
   ```

2. **Deteksi `inferredPlatform` dan `viewport` dari Event `Meta` (`type: 4`)**:
   - Periksa seluruh event `type === EventType.Meta`.
   - Jika `data.href` diawali `android://` $\rightarrow$ `inferredPlatform = 'android'`.
   - Jika `data.href` diawali `ios://` $\rightarrow$ `inferredPlatform = 'ios'`.
   - Simpan `width` dan `height` dari event `Meta` pertama (mis. `{ width: 320, height: 716 }`) ke dalam `model.viewport`.

3. **Injeksi CSS Wireframe untuk Snapshot Synthetic Mobile (`data-artracker-type`)**:
   Buat fungsi helper `enhanceMobileSyntheticSnapshot(events: eventWithTime[])` di [`normalize.ts`](file:///Users/tiofani/IdeaProjects/artracker/frontend/src/replay/normalize.ts) yang berjalan ketika snapshot mengandung atribut `data-artracker-type` atau `inferredPlatform === 'android' | 'ios'`:
   - **Sisipkan `<style>` ke dalam node `<head>`** pada setiap `EventType.FullSnapshot` (`type: 2`) jika `<head>` belum memiliki stylesheet:
     ```css
     html, body {
       margin: 0 !important;
       padding: 0 !important;
       font-family: 'Roboto', -apple-system, BlinkMacSystemFont, 'Inter', sans-serif !important;
       font-size: 12px !important;
       line-height: 1.25 !important;
       color: #0f172a !important;
       background: #fafafa !important;
       -webkit-font-smoothing: antialiased !important;
     }
     [data-artracker-type="CONTAINER"] {
       box-sizing: border-box !important;
     }
     [data-artracker-type="TEXT"] {
       display: flex !important;
       align-items: center !important;
       font-family: 'Roboto', -apple-system, BlinkMacSystemFont, sans-serif !important;
       font-size: 11px !important;
       line-height: 1.15 !important;
       letter-spacing: -0.01em !important;
       color: #1e293b !important;
       overflow: visible !important;
       white-space: nowrap !important;
     }
     [data-artracker-type="BUTTON"] {
       background: rgba(0, 122, 255, 0.10) !important;
       border: 1px solid rgba(0, 122, 255, 0.30) !important;
       border-radius: 8px !important;
       box-sizing: border-box !important;
     }
     [data-artracker-type="BUTTON"] [data-artracker-type="TEXT"] {
       color: #0066dc !important;
       font-weight: 600 !important;
       font-size: 10.5px !important;
       justify-content: center !important;
     }
     [data-artracker-type="IMAGE"] {
       background: rgba(148, 163, 184, 0.18) !important;
       border: 1px dashed rgba(100, 116, 139, 0.35) !important;
       border-radius: 6px !important;
       box-sizing: border-box !important;
     }
     ```
   - **Pemberian Surface / Border Otomatis pada Struktur Layout Android (`CONTAINER`)**:
     Saat menelusuri pohon `FullSnapshot` dan `IncrementalSnapshot` (`adds`):
     - **Card / Section Container**: Untuk `CONTAINER` dengan lebar `260px–305px` dan tinggi `>= 48px` (seperti kartu `"Checkout e-commerce"`, `"Pendaftaran bertahap"`, `"Event & log"`, `"Kontrol replay"`), tambahkan style inline `background-color:#ffffff;border:1px solid #e2e8f0;border-radius:12px;box-shadow:0 1px 2px rgba(15,23,42,0.04);`.
     - **Top Bar / Header**: Untuk `CONTAINER` dengan `left: 0px`, `width: 320px`, `top <= 60px`, dan `height <= 64px`, tambahkan `background-color:#ffffff;border-bottom:1px solid #e2e8f0;`.
     - **Bottom Navigation Bar**: Untuk `CONTAINER` di bagian bawah layar (`top >= 560px`, `width: 320px`), tambahkan `background-color:#ffffff;border-top:1px solid #e2e8f0;`. Untuk kotak indikator pill `64×32px` di dalamnya yang memiliki sibling teks tab, beri border halus atau ikon placeholder agar item navigasi (`Skenario`, `Lab`, `Inspector`, `Pengaturan`) tampak jelas.

4. **Resolusi *Double-Screen Ghosting* pada Transisi Compose (`normalize.ts`)**:
   - Di dalam satu `FullSnapshot` (`type: 2`), periksa apakah terdapat dua child `CONTAINER` langsung di bawah satu parent yang memiliki bounding box halaman utama yang identik (`left`, `top`, `width >= 300`, `height >= 350` sama persis — contoh pada sesi ini: `div#21` dan `div#54` di `[0, 160, 320, 432]`).
   - Jika terdeteksi (atau jika mutasi `IncrementalSnapshot` berikutnya langsung menghapus child pertama tersebut), tambahkan `background-color:#fafafa;z-index:2;` pada child yang lebih baru (`div#54` / `div#48`) dan `display:none;` pada child lama (`div#21`) yang sedang keluar (*exiting*). Hasilnya: tidak ada lagi dua layar yang saling bertumpuk.

5. **Kompatibilitas Chunk Mobile Tanpa Properti `format` (`normalize.ts:100-127`)**:
   - Pada `extractMobileFrames()`, ubah pengecekan:
     ```ts
     const isMobilePayload =
       p.format === 'artracker-mobile-v1' ||
       ((p.platform === 'ios' || p.platform === 'android') && Array.isArray(p.frames))
     ```
     agar chunk iOS/Android yang tidak menyertakan string `"format": "artracker-mobile-v1"` tetap dirender dengan `MobileWireframeRenderer`.

---

### Tugas 2: Perbaikan Klasifikasi Frame, Proporsi Viewport Android, & Route Strip Dinamis (`frontend/src/components/ReplayPlayer.tsx`)

**File**: [`frontend/src/components/ReplayPlayer.tsx`](file:///Users/tiofani/IdeaProjects/artracker/frontend/src/components/ReplayPlayer.tsx)

1. **Perbaiki Penentuan `effectivePlatform` (`ReplayPlayer.tsx:200-208`)**:
   Jangan memaksa `'web'` hanya karena `model.kind === 'web'`. Gunakan urutan prioritas berikut ketika `deviceMode === 'auto'`:
   ```ts
   const effectivePlatform: DetectedPlatform =
     deviceMode !== 'auto'
       ? deviceMode
       : model.kind === 'mobile'
         ? model.platform
         : model.kind === 'web' && model.inferredPlatform && model.inferredPlatform !== 'web'
           ? model.inferredPlatform
           : detection.platform !== 'unknown'
             ? detection.platform
             : 'web'
   ```
   Dengan perubahan ini, sesi `01a1bf8…2cefb` pada mode `Auto` akan langsung menampilkan bingkai **`.or-phone-frame--android`**!

2. **Sesuaikan Dimensi `playerWidth` & `playerHeight` dengan Rasio Kanvas Mobile (`ReplayPlayer.tsx:223-229`)**:
   - Untuk `effectivePlatform === 'android'` atau `'ios'` ketika `model.kind === 'web'`:
     - Jika `model.viewport` tersedia (mis. `320 × 716`), hitung lebar/tinggi panggung dalam bingkai ponsel agar sesuai dengan rasio aspek kanvas asli (mis. tinggi bersih area layar ponsel `550px` $\rightarrow$ lebar bersih `Math.round(550 * (320 / 716)) = 246px`, atau sesuaikan lebar dalam `.or-phone-frame--android` menjadi `250px × 560px` untuk rasio `9:20` modern Android).
     - Tampilkan resolusi asli dari `model.viewport` (`320 × 716`) pada chip `.or-ChromeResolution` di kanan atas player, bukan `1440 × 900` atau `412 × 915` statis.

3. **Buat Route / Screen Strip (`recordedUrl`) Dinamis Mengikuti Waktu Putar (`ReplayPlayer.tsx:258-281`)**:
   - Ganti `useMemo` `recordedUrl` yang saat ini hanya membaca event `Meta` pertama menjadi pencari event `EventType.Meta` terakhir yang memiliki `ev.timestamp <= baseTimestampMs + currentTimeMs`:
     ```ts
     const recordedUrl = useMemo(() => {
       if (model.kind !== 'web') return null
       const targetTs = baseTimestampMs + currentTimeMs
       let currentHref: string | null = null
       for (const ev of model.events) {
         if (ev.type === EventType.Meta) {
           const data = ev.data as { href?: string }
           if (data?.href) {
             if (ev.timestamp <= targetTs + 50 || currentHref === null) {
               currentHref = data.href
             } else {
               break
             }
           }
         }
       }
       return currentHref
     }, [model, baseTimestampMs, currentTimeMs])
     ```
   - Jika `effectivePlatform === 'android'` atau `recordedUrl?.startsWith('android://')`:
     - Ubah label chip dari `URL` menjadi **`SCREEN`**.
     - Format tampilan route `android://lab/_id_` menjadi `android://lab/{id}` atau `Screen · lab/{id}` agar seragam dengan tampilan iOS (`Screen`).

4. **Perbaiki Kalkulasi Posisi Marker Timeline (`ReplayPlayer.tsx:302-333`)**:
   - Pada `timelineMarkers`:
     - Jika `evTs > 0 && baseTimestampMs > 0`:
       - Hitung `rawOffset = evTs - baseTimestampMs`.
       - Jika `rawOffset < 0` (event terjadi beberapa detik sebelum chunk pertama dimulai, seperti `app_foreground` pada `15:52:46` vs chunk pertama `15:52:50`), clamp ke `offsetMs = 0` (jangan masuk ke rumus fallback proporsional `(idx + 1) / (length + 1)` yang melempar event ke tengah video).
       - Gunakan rumus proporsional **hanya** jika `evTs <= 0 || baseTimestampMs <= 0`.

---

### Tugas 3: Perbaikan CSS Phone Frame & Kartu Session Queue (`frontend/src/dashboard.css`)

**File**: [`frontend/src/dashboard.css`](file:///Users/tiofani/IdeaProjects/artracker/frontend/src/dashboard.css)

1. **Hilangkan Letterboxing Hitam di Dalam Bingkai Ponsel (`.or-phone-frame`)**:
   - Pada [`dashboard.css`](file:///Users/tiofani/IdeaProjects/artracker/frontend/src/dashboard.css#L2423-L2435), `.replay-rrweb-mount` dan `.rr-player` saat ini menggunakan `background: var(--color-graphite)` (hitam/abu gelap).
   - Tambahkan aturan khusus ketika berada di dalam `.or-phone-frame`:
     ```css
     .or-phone-frame .replay-rrweb-mount,
     .or-phone-frame .replay-rrweb-mount .rr-player,
     .or-phone-frame .replay-rrweb-mount .rr-player__frame {
       background: #fafafa;
     }
     ```
   - Sesuaikan ukuran `.or-phone-frame--android` (`width: 262px; height: 608px;` atau proporsi dalam `250px × 560px`) agar rasio ruang layar bagian dalam (`250 / 560 = 0,4464`) sama persis dengan rasio layar Android `320 × 716` (`320 / 716 = 0,4469`). Dengan demikian kanvas mengisi 100% ruang bingkai ponsel tanpa celah di kiri-kanan!

2. **Perbaiki Indikator `.dash-session-item__dot--clean` (`dashboard.css:1265-1268`)**:
   - Ganti variabel yang tidak terdefinisi:
     ```css
     .dash-session-item__dot--clean {
       background: var(--color-success);
       box-shadow: 0 0 0 2px var(--color-success-wash);
     }
     ```

3. **Bersihkan Duplikasi & Kontras Badge `.dash-session-item__platform` (`dashboard.css:1280-1291` & `1734-1745`)**:
   - Gunakan token desain semantik yang kontras di mode terang maupun gelap:
     ```css
     .dash-session-item__platform {
       font-family: var(--font-mono);
       font-size: 9.5px;
       font-weight: 600;
       padding: 1px 6px;
       border-radius: 4px;
       background: var(--color-paper-3);
       color: var(--color-ink-2);
       border: 1px solid var(--color-rule-2);
       letter-spacing: 0.02em;
       flex-shrink: 0;
     }
     ```

---

## 3. Kriteria Penerimaan (Acceptance Criteria — Web)

1. Saat membuka sesi `01a1bf82-03b1-7c23-8acc-75ac4b72cefb` dengan mode viewport **`Auto`**:
   - Bingkai yang tampil adalah **Android Phone Frame (`.or-phone-frame--android`)**, bukan Desktop Browser.
   - Chip resolusi menampilkan **`320 × 716`**.
   - Kanvas pas memenuhi layar ponsel tanpa blok hitam di kiri/kanan.
2. Seluruh teks di dalam rekaman Android tampil menggunakan font sans-serif (**Roboto / SF Pro / Inter**) berukuran proporsional tanpa terpotong (`Status SDK: memuat…`, `Antrean & transport`, `EVENTS`, `SCREENS`, `MASKING`).
3. Seluruh 29 tombol (`Jalankan`, `Mulai replay`, `Hentikan replay`, chip filter, tab) memiliki kotak visual tombol berwarna biru muda dengan teks tebal yang jelas, dan kartu skenario/lab memiliki border kartu yang bersih.
4. Pada detik `00:14` (`android://lab`) dan `00:18` (`android://lab/_id_`), layar lama tidak lagi menembus/menimpa layar baru (*zero ghosting*).
5. Bar informasi layar di atas player berubah secara dinamis dari `MainActivity` $\rightarrow$ `skenario` $\rightarrow$ `lab` $\rightarrow$ `lab/{id}` seiring berjalannya waktu putar.
6. Bulatan hijau `.dash-session-item__dot--clean` dan badge `Android` di daftar **Session Queue** terlihat jelas pada tema terang maupun gelap.
7. Build TypeScript & Vite (`npm run build` di `frontend/`) berjalan sukses tanpa error.
