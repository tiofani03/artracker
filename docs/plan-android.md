# Rencana Implementasi Perbaikan — Android SDK (`artracker-android`)

> **Target Repositori**: `artracker-android` (SDK Android / Jetpack Compose & View Recorder)  
> **Konteks Audit**: Sesi Android `01a1bf82-03b1-7c23-8acc-75ac4b72cefb` (`ai-mastery-android`, `project_id = 5`, `sdk_version = 0.1.0`)  
> **Dokumen Terkait**: [Audit Utama (`docs/plan.md`)](file:///Users/tiofani/IdeaProjects/artracker/docs/plan.md) · [Plan Web Dashboard (`docs/plan-web.md`)](file:///Users/tiofani/IdeaProjects/artracker/docs/plan-web.md)  
> **Tujuan**: Memperbaiki kualitas perekaman pohon tampilan (*view/semantics tree*), mencegah terekamnya dua layar bertumpuk saat transisi navigasi Jetpack Compose, menyertakan informasi gaya visual/semantik komponen, dan menyelaraskan metadata durasi sesi serta format payload dengan ekosistem `artracker`.

---

## 1. Daftar Masalah di Sisi SDK Android (`artracker-android`)

Berdasarkan dekompresi 3 chunk (`chunk_1.gz`, `chunk_2.gz`, `chunk_3.gz`) dari sesi `01a1bf82-03b1-7c23-8acc-75ac4b72cefb`, berikut adalah cacat perekaman yang berasal langsung dari SDK Android:

| ID | Masalah pada Payload yang Dikirim SDK Android | Bukti pada Data Chunk (`01a1bf8…2cefb`) | Prioritas |
| :--- | :--- | :--- | :---: |
| **AND-01** | **`FullSnapshot` Direkam Saat Transisi `NavHost` Masih Berjalan (*Mid-Transition Snapshot*)**: Ketika berpindah route Jetpack Compose (`skenario` $\rightarrow$ `lab` dan `lab` $\rightarrow$ `lab/{id}`), SDK langsung mengambil `FullSnapshot` saat animasi *crossfade/slide* baru dimulai. Akibatnya, node layar lama (`exiting`) dan layar baru (`entering`) sama-sama terekam di koordinat `[0, 160, 320, 432]` selama ~2,03 detik. | `chunk_1` event `[12]` (`ts=1794214384360`): `div#21` (`skenario`) & `div#54` (`lab`) hidup bersamaan.<br>`chunk_2` event `[5]` (`ts=1794214388970`): `div#21` (`lab`) & `div#48` (`lab/{id}`) hidup bersamaan. | **P0 (Kritis)** |
| **AND-02** | **DOM Sintetis Tidak Menyertakan `<style>` maupun Properti Visual Komponen**: Dari 323 node yang dikirim (`183 CONTAINER`, `110 TEXT`, `29 BUTTON`, `1 IMAGE`), `<head>` (`id: 3`) kosong (`childNodes: []`) dan 320 node hanya memiliki `position:absolute;left:Xpx;top:Ypx;width:Wpx;height:Hpx;overflow:hidden;`. Tidak ada `font-family`, `font-size`, `color`, `background-color`, `border`, atau `border-radius`. | Seluruh `FullSnapshot` di `chunk_1.gz` & `chunk_2.gz` memiliki `<head></head>` kosong dan `<div data-artracker-type="BUTTON">` tanpa warna/border. | **P0 (Kritis)** |
| **AND-03** | **Tinggi Bounding Box `TEXT` Terlalu Sempit + `overflow: hidden`**: Beberapa node teks di Jetpack Compose memiliki tinggi terukur sangat kecil (`7px`–`15px`) karena clipping layout parent atau *font padding*, lalu diberi `overflow: hidden` tanpa `font-size` yang sesuai. | `span#46` (`"Antrean & transport"`, `129×7px`), `span#19` (`"Status SDK: memuat…"`, `112×15px`), `span#29` (`"EVENTS"`, `47×15px`). | **P1 (Mayor)** |
| **AND-04** | **Kurangnya Klasifikasi Tipe Komponen Semantik (`data-artracker-type`)**: Seluruh `Card`, `Surface`, `TopAppBar`, `NavigationBar` (Bottom Nav), `NavigationBarItem`, `FilterChip`, dan `TextField` hanya dilabeli sebagai `CONTAINER` atau `BUTTON` generik, dan ikon tab (`24×24px`) direkam sebagai `CONTAINER` kosong. | `div#59` / `div#82` (Bottom Navigation Bar) memiliki 4 kotak ikon `24×24px` (`div#65`, `71`, `77`, `83`) bertipe `CONTAINER` kosong tanpa label/ikon. | **P1 (Mayor)** |
| **AND-05** | **Selisih `duration_ms` Sesi (`66.969 ms` / `01:07`) vs Rentang Waktu Chunk (`32.805 ms` / `00:32`)**: Sesi dimulai pada `15:52:15` (saat inisialisasi SDK), sedangkan chunk pertama baru direkam pada `15:52:50` dan chunk terakhir selesai pada `15:53:22`, sementara `duration_ms` dihitung hingga `app_background` berikutnya. | `replay_sessions.duration_ms = 66969`, sedangkan `chunk_3.end - chunk_1.start = 32805`. | **P2 (Menengah)** |

---

## 2. Langkah Implementasi Detail di Repo `artracker-android`

### Tugas 1: Tunggu Animasi Transisi `NavHost` / `HorizontalPager` Selesai Sebelum Mengambil `FullSnapshot` (Solusi `AND-01`)

**Masalah**:
Saat `NavController.OnDestinationChangedListener` terpanggil di Jetpack Compose, `NavHost` baru saja memulai animasi transisi (`EnterTransition` / `ExitTransition` default ~300–700ms). Pada detik itu, komposisi masih memegang dua subtree `SemanticsNode` sekaligus: layar yang sedang keluar (`exiting`) dan layar yang sedang masuk (`entering`). Karena interval *polling* mutasi berikutnya baru berjalan ~2 detik kemudian, kedua layar bertahan di dalam snapshot selama 2 detik penuh.

**Langkah Perbaikan di SDK Android**:
1. **Jangan langsung memicu `captureFullSnapshot()` tepat di dalam callback sinkron `OnDestinationChangedListener`**.
2. **Jadwalkan ulang pengambilan snapshot setelah frame transisi selesai**:
   - Tunggu hingga daftar `visibleEntries` pada `NavController.currentBackStackEntryFlow` / `NavHost` hanya menyisakan **1 destination aktif** (atau gunakan debounce `350ms` + `withFrameNanos {}` setelah navigasi), **ATAU**
   - Saat men-serialize pohon `SemanticsNode`, periksa apakah di bawah root container `NavHost` terdapat lebih dari satu subtree route layar utama yang memiliki ukuran identik (`width == parent.width && height == parent.height`):
     - Jika ada >1 subtree route layar, **hanya serialize subtree terakhir (urutan gambar paling atas / `entering` destination)** dan abaikan subtree pertama (`exiting` destination), serta pastikan root container layar aktif diberi `background-color` opaque (`#fafafa` atau warna `MaterialTheme.colorScheme.background`).
3. **Percepat Interval Flush Mutasi Pasca-Navigasi**:
   - Setelah event navigasi terjadi, lakukan pemeriksaan diff/mutasi ekstra pada `+400ms` setelah `FullSnapshot` agar setiap perubahan layout pasca-animasi langsung terkirim tanpa menunggu siklus 2 detik berikutnya.

---

### Tugas 2: Sertakan Default Wireframe `<style>` dan Atribut Visual pada Serializer `rrweb` (Solusi `AND-02` & `AND-03`)

Jika SDK Android tetap menggunakan jalur serializer `rrweb` (`type: 4` Meta, `type: 2` FullSnapshot, `type: 3` IncrementalSnapshot) untuk mendukung *incremental mutation* & *smooth scrolling*:

1. **Sertakan Node `<style>` di Dalam `<head>` (`id: 3`) pada Setiap `FullSnapshot`**:
   Saat membangun struktur `<html> -> <head> -> <body>`, tambahkan satu child element `<style>` di dalam `<head>` sehingga chunk Android mandiri (*self-contained*) di player `rrweb` mana pun:
   ```css
   html, body {
     margin: 0;
     padding: 0;
     font-family: Roboto, -apple-system, BlinkMacSystemFont, sans-serif;
     font-size: 12px;
     line-height: 1.25;
     color: #0f172a;
     background: #fafafa;
   }
   [data-artracker-type] {
     box-sizing: border-box;
   }
   [data-artracker-type="TEXT"] {
     display: flex;
     align-items: center;
     font-size: 11px;
     line-height: 1.15;
     color: #1e293b;
     white-space: nowrap;
   }
   [data-artracker-type="BUTTON"],
   [data-artracker-type="CHIP"] {
     background: rgba(0, 122, 255, 0.10);
     border: 1px solid rgba(0, 122, 255, 0.30);
     border-radius: 8px;
   }
   [data-artracker-type="CARD"] {
     background: #ffffff;
     border: 1px solid #e2e8f0;
     border-radius: 12px;
   }
   [data-artracker-type="TOOLBAR"],
   [data-artracker-type="TABBAR"] {
     background: #ffffff;
     border-color: #e2e8f0;
   }
   ```

2. **Ekstrak Ukuran Font & Warna Asli dari `SemanticsNode` (`TextLayoutResult`)**:
   - Pada Compose `SemanticsNode`, ambil `SemanticsActions.GetTextLayoutResult`:
     ```kotlin
     val textLayoutResults = mutableListOf<TextLayoutResult>()
     node.config.getOrNull(SemanticsActions.GetTextLayoutResult)?.action?.invoke(textLayoutResults)
     val style = textLayoutResults.firstOrNull()?.layoutInput?.style
     ```
   - Konversi `style.fontSize` (dalam `sp`/`px` yang sudah dibagi `density`) ke properti CSS `font-size: ${fontSizeDp}px;`, `font-weight: ${weight};`, dan `color: ${hexColor};`.
   - **Perbaikan `overflow` pada `TEXT`**: Jangan memasang `overflow: hidden` pada `<span data-artracker-type="TEXT">` jika `height < 16dp` (seperti `span#46` yang tingginya terpotong menjadi `7px` saat sedang di-scroll di tepi viewport). Gunakan `overflow: hidden` pada container scroll parent-nya saja (`LazyColumn` / `Column` yang memiliki `VerticalScrollAxisRange`), sehingga teks di luar batas scroll otomatis ter-clip oleh container scroll sebagaimana mestinya, bukan terpotong setengah huruf oleh span-nya sendiri!

---

### Tugas 3: Perkaya Atribut Semantik `data-artracker-type` & `data-artracker-role` (Solusi `AND-04`)

Saat memetakan `SemanticsNode` Jetpack Compose atau `android.view.View` ke elemen DOM:

| Kondisi pada `SemanticsNode` / `View` | `data-artracker-type` | Styling / Atribut Tambahan yang Direkomendasikan |
| :--- | :--- | :--- |
| Memiliki `SemanticsProperties.Role == Role.Button` atau `OnClick` | `BUTTON` | `border-radius: 8px;` |
| Memiliki `SemanticsProperties.Role == Role.Tab` | `TAB` | Tambahkan `data-artracker-selected="true\|false"` dari `SemanticsProperties.Selected` |
| Memiliki `SemanticsProperties.Role == Role.Checkbox` / `Switch` / `RadioButton` | `TOGGLE` | Tambahkan `data-artracker-checked="true\|false"` dari `ToggleableState` |
| Memiliki `SemanticsProperties.EditableText` / `SetText` | `INPUT` | `background:#f8fafc;border:1px solid #cbd5e1;border-radius:6px;` |
| Memiliki `SemanticsProperties.VerticalScrollAxisRange` / `HorizontalScrollAxisRange` | `SCROLL_CONTAINER` | Pastikan `overflow: hidden;` dipasang di node ini untuk meng-clip item list di dalamnya |
| Memiliki `SemanticsProperties.ContentDescription` atau `Role == Role.Image` | `IMAGE` / `ICON` | Simpan `data-artracker-label="<contentDescription>"` agar ikon navigasi bawah (`24×24px`) tidak menjadi kotak kosong |
| Memiliki teks yang di-mask karena aturan privasi (PII) | `TEXT` / `INPUT` | Tambahkan `data-artracker-masked="true"` agar player dapat menghitung dan menampilkan badge `🛡️ masked` seperti di iOS |

---

### Tugas 4: Sinkronisasi Format URI Route (`android://`) & Durasi Sesi (Solusi `AND-05`)

1. **Jangan Mengubah `{id}` Menjadi `_id_` pada `Meta.data.href`**:
   - Pada `telemetry_events`, SDK mengirim `"screen": "lab/{id}"`, tetapi pada chunk `rrweb` event `Meta` (`type: 4`), SDK mengubahnya menjadi `"href": "android://lab/_id_"`.
   - Pertahankan format route asli (`"href": "android://lab/{id}"`) agar cocok 100% ketika dibandingkan dengan event `screen_view` di `telemetry_events`.
2. **Sinkronisasi Timestamp Awal Sesi (`start_time`) dan `duration_ms`**:
   - Pastikan perekaman chunk pertama (`FullSnapshot` awal) langsung dipicu saat sesi replay dimulai (`startSession()`), dan saat sesi dihentikan (`stopSession()` / `onBackground`), hitung `duration_ms` berdasarkan `lastEventTimestamp - firstEventTimestamp` dari sesi replay yang aktif agar angka durasi di **Session Queue** (`replay_sessions.duration_ms`) selalu sama dengan panjang pemutaran di **ReplayPlayer**.

---

## 3. Opsi Jangka Menengah: Penyelarasan ke Skema `artracker-mobile-v1` (Opsional)

Apabila tim ingin menyatukan pipeline rendering iOS dan Android ke dalam satu engine (`MobileWireframeRenderer.tsx`) tanpa `rrweb-player`:
- SDK Android dapat menyediakan mode output JSON `artracker-mobile-v1` yang identik dengan SDK iOS:
  ```json
  {
    "format": "artracker-mobile-v1",
    "platform": "android",
    "frames": [
      {
        "ts": 1794214370102,
        "screen": "skenario",
        " masked_count": 0,
        "view_tree": {
          "cls": "ComposeView",
          "frame": [0, 0, 320, 716],
          "children": [...]
        }
      }
    ],
    "touches": [
      { "ts": 1794214384118, "type": "tap", "x": 120, "y": 654, "screen": "skenario" }
    ]
  }
  ```
- **Catatan Arsitektur**: Selama SDK Android masih menggunakan format `rrweb` sintetis, implementasi **Tugas 1 s/d Tugas 4** di atas sudah cukup untuk membuat hasil rekaman `rrweb` Android tampil bersih, tajam, bebas *ghosting*, dan tetap memiliki keunggulan animasi scroll 60fps yang mulus.

---

## 4. Kriteria Penerimaan (Acceptance Criteria — Android SDK)

1. Saat berpindah antar layar di `NavHost` (`MainActivity` $\rightarrow$ `skenario` $\rightarrow$ `lab` $\rightarrow$ `lab/{id}`), `FullSnapshot` yang dihasilkan hanya berisi pohon `SemanticsNode` dari layar tujuan yang aktif (tidak ada dua container layar `320×432` yang hidup bersamaan).
2. Setiap `FullSnapshot` memiliki `<style>` default di dalam `<head>`, dan setiap `<span data-artracker-type="TEXT">` memiliki `font-size` proporsional dari `TextLayoutResult` tanpa terpotong oleh `overflow: hidden` pada kotak teks yang sempit.
3. Tombol, tab aktif (`NavigationBarItem`), kartu (`Card`/`Surface`), dan ikon (`Role.Image`) memiliki atribut `data-artracker-type` / `data-artracker-selected` yang jelas.
4. `duration_ms` yang dikirim ke endpoint sesi sesuai dengan rentang timestamp chunk yang diunggah ke R2.
