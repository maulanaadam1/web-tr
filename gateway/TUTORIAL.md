# 📖 Tutorial Lengkap: Web-TR Tunnel Gateway

Aplikasi desktop Windows untuk meneruskan (streaming) kamera CCTV/NVR lokal ke server VPS melalui jaringan **ZeroTier**, sehingga kamera Anda bisa diakses dari mana saja di dunia melalui dashboard web.

---

## 📋 Daftar Isi

1. [Prasyarat](#-1-prasyarat)
2. [Instalasi ZeroTier](#-2-instalasi-zerotier)
3. [Menjalankan Aplikasi](#-3-menjalankan-aplikasi)
4. [Konfigurasi Settings](#-4-konfigurasi-settings)
5. [Menambah Kamera](#-5-menambah-kamera)
6. [Import Kamera via CSV](#-6-import-kamera-via-csv)
7. [Menjalankan Stream](#-7-menjalankan-stream-start-all)
8. [Monitoring & Log](#-8-monitoring--log)
9. [System Tray](#-9-system-tray--minimize)
10. [Diagnose VPS](#-10-diagnose-vps)
11. [Open Dashboard](#-11-open-dashboard)
12. [Troubleshooting](#-12-troubleshooting)

---

## 🔧 1. Prasyarat

Sebelum menggunakan aplikasi ini, pastikan Anda memiliki:

| Komponen | Keterangan |
|---|---|
| **Windows 10/11** | Sistem operasi PC yang menjalankan aplikasi |
| **ZeroTier** | VPN mesh yang menghubungkan PC ↔ VPS ([Download](https://www.zerotier.com/download/)) |
| **NVR / IP Camera** | Perangkat CCTV yang mendukung RTSP, terhubung di jaringan lokal yang sama dengan PC |
| **VPS** | Server cloud yang sudah terinstal `go2rtc` dan `ZeroTier` |

---

## 🌐 2. Instalasi ZeroTier

ZeroTier adalah "jembatan ajaib" yang menghubungkan PC Anda dengan VPS tanpa perlu membuka port di router atau firewall.

### Langkah-langkah:

1. **Buat akun** di [my.zerotier.com](https://my.zerotier.com/)
2. Klik **Create A Network** → catat **Network ID** (16 karakter, contoh: `ebe7fbd445fb5bed`)
3. **Download & instal** ZeroTier di PC Anda dari [zerotier.com/download](https://www.zerotier.com/download/)
4. Klik kanan ikon ZeroTier di **System Tray** (pojok kanan bawah) → **Join New Network** → masukkan Network ID
5. Buka kembali [my.zerotier.com](https://my.zerotier.com/), klik jaringan Anda, scroll ke **Members**, lalu **centang Auth** untuk PC dan VPS Anda

> [!IMPORTANT]
> Pastikan **kedua perangkat** (PC dan VPS) sudah bergabung ke Network ID yang sama dan sudah di-**Authorize** di dashboard ZeroTier!

---

## 🚀 3. Menjalankan Aplikasi

1. Klik dua kali file **`Web-TR-Gateway.exe`**
2. Aplikasi akan terbuka dengan tampilan **Dashboard** utama

> [!TIP]
> Aplikasi memiliki proteksi **Single Instance** — jika Anda tidak sengaja mengklik 2x atau 3x, hanya satu jendela yang akan terbuka. Instance duplikat akan otomatis tertutup.

### Tampilan Utama

Aplikasi memiliki **2 tab utama**:

| Tab | Fungsi |
|---|---|
| **🏠 Dashboard** | Kontrol kamera, monitoring, dan log aktivitas |
| **⚙️ Settings** | Pengaturan IP VPS |

---

## ⚙️ 4. Konfigurasi Settings

Sebelum mulai streaming, pastikan IP VPS sudah benar:

1. Klik tab **Settings**
2. Masukkan **IP VPS** Anda di kolom "VPS Server IP" (contoh: `43.157.204.11`)
3. Klik **Save Settings**

> [!NOTE]
> Konfigurasi disimpan otomatis ke file `config.json` di folder yang sama dengan aplikasi.

---

## 📷 5. Menambah Kamera

### Cara Manual (Satu per Satu):

1. Klik tombol ikon **➕** (Add Camera) di toolbar
2. Isi:
   - **Camera Name**: Nama identifikasi kamera (contoh: `Kamera Depan`)
   - **Local RTSP URL**: Alamat RTSP kamera Anda (contoh: `rtsp://admin:password@192.168.1.10:554/live`)
3. Klik **Save**

### Cara Mendapatkan RTSP URL

RTSP URL berbeda-beda tergantung merk kamera/NVR:

| Merk | Contoh Format RTSP |
|---|---|
| **Hikvision** | `rtsp://admin:password@IP:554/Streaming/Channels/101` |
| **Dahua** | `rtsp://admin:password@IP:554/cam/realmonitor?channel=1&subtype=0` |
| **Vivotek** | `rtsp://root:password@IP:554/live1s2.sdp` |
| **Generic ONVIF** | `rtsp://admin:password@IP:554/stream1` |

---

## 📁 6. Import Kamera via CSV

Untuk menambahkan banyak kamera sekaligus:

1. Klik tombol ikon **📄** (Import CSV) di toolbar
2. Pilih file `.csv` dari komputer Anda
3. Kamera yang valid akan otomatis ditambahkan

### Format File CSV

Buat file `.csv` dengan format berikut (bisa menggunakan Notepad atau Excel):

```csv
Name,LocalRTSP
Kamera Depan,rtsp://admin:admin123@192.168.1.10:554/cam/realmonitor?channel=1&subtype=0
Kamera Belakang,rtsp://admin:admin123@192.168.1.11:554/cam/realmonitor?channel=1&subtype=0
Kamera Gudang,rtsp://admin:admin123@192.168.1.12:554/live1s2.sdp
Kamera Parkir,rtsp://admin:admin123@192.168.1.13:554/Streaming/Channels/101
```

**Aturan:**
- Baris pertama **wajib** header: `Name,LocalRTSP`
- Setiap baris berikutnya: `NamaKamera,AlamatRTSP`
- Pisahkan dengan tanda koma (`,`)
- Jangan gunakan spasi setelah koma

> [!TIP]
> File contoh `sample_cameras.csv` sudah tersedia di folder aplikasi sebagai referensi.

---

## ▶️ 7. Menjalankan Stream (Start All)

1. Pastikan **ZeroTier** sudah aktif dan terhubung di PC Anda
2. Klik tombol **▶ Start All**

### Apa yang Terjadi di Balik Layar:

```mermaid
sequenceDiagram
    participant PC as 💻 PC Anda
    participant ZT as 🌐 ZeroTier
    participant VPS as ☁️ VPS
    participant CAM as 📷 Kamera/NVR

    PC->>PC: Deteksi IP ZeroTier otomatis
    PC->>PC: Buat TCP Proxy per kamera
    PC->>CAM: Hubungkan proxy ke RTSP kamera
    PC->>VPS: Daftarkan stream via API go2rtc
    VPS->>ZT: Akses stream melalui ZeroTier
    ZT->>PC: Teruskan ke TCP Proxy
    PC->>CAM: Ambil data video RTSP
```

**Penjelasan:**
- Aplikasi otomatis mendeteksi IP ZeroTier PC Anda
- Untuk setiap kamera, aplikasi membuat **TCP Proxy** lokal (port otomatis)
- Stream didaftarkan ke VPS menggunakan IP ZeroTier + port proxy
- VPS mengakses kamera melalui jalur: **VPS → ZeroTier → PC → TCP Proxy → NVR/Kamera**

### Indikator Status:

| Warna Dot | Status |
|---|---|
| 🟢 Hijau | Kamera aktif dan terhubung |
| 🔴 Merah | Kamera mati / belum dimulai |

### Menghentikan Stream

- Klik **■ Stop All** untuk menghentikan semua kamera sekaligus
- Atau klik tombol **■** (stop) pada masing-masing kamera secara individual

---

## 📊 8. Monitoring & Log

### Status Bar (Header)

Di bagian atas Dashboard, Anda bisa melihat informasi real-time:
- **💻 CPU**: Penggunaan prosesor saat ini
- **🔼 Up**: Kecepatan upload jaringan
- **🔽 Down**: Kecepatan download jaringan

### Live Activity Log

Di bagian bawah Dashboard, terdapat area **Live Activity** yang menampilkan semua log aktivitas secara real-time, termasuk:
- Status koneksi ZeroTier
- Proses registrasi stream ke VPS
- Error atau peringatan

---

## 🔽 9. System Tray / Minimize

Aplikasi dirancang untuk berjalan di latar belakang:

- **Klik tombol ✕ (Close)** → Aplikasi **TIDAK ditutup**, melainkan **tersembunyi ke System Tray** (area ikon kecil di pojok kanan bawah, dekat jam)
- **Klik kanan ikon di System Tray**:
  - **Show** → Tampilkan kembali jendela aplikasi
  - **Quit** → Tutup aplikasi sepenuhnya

> [!IMPORTANT]
> Selama aplikasi berjalan di System Tray, semua stream kamera **tetap aktif** dan terus berjalan di latar belakang!

---

## 🔍 10. Diagnose VPS

Klik tombol ikon **🔍** (Diagnose) di toolbar untuk memeriksa konektivitas ke VPS:
- Mengecek apakah VPS bisa dijangkau melalui HTTP
- Menampilkan hasil diagnosis di area log

---

## 🌐 11. Open Dashboard

Klik tombol ikon **🖥️** (Open Dashboard) untuk membuka dashboard web `go2rtc` di browser default Anda. Di sana Anda bisa:
- Melihat semua stream kamera yang terdaftar
- Memutar live preview kamera
- Mengelola stream dari antarmuka web

---

## 🛠️ 12. Troubleshooting

### ❌ "ZeroTier IP not found"
- Pastikan aplikasi ZeroTier sudah terinstal dan aktif
- Pastikan sudah join ke Network ID yang benar
- Pastikan perangkat sudah di-Authorize di dashboard ZeroTier

### ❌ "Connection refused" saat Start All
- Pastikan VPS menyala dan `go2rtc` sedang berjalan
- Pastikan VPS juga sudah bergabung ke jaringan ZeroTier yang sama
- Cek apakah IP VPS di Settings sudah benar

### ❌ Kamera tidak muncul di Dashboard Web
- Pastikan RTSP URL kamera benar (bisa ditest dulu di VLC Player)
- Pastikan kamera dan PC berada di jaringan lokal yang sama
- Periksa username/password di RTSP URL

### ❌ Aplikasi terbuka lebih dari satu
- Ini sudah dicegah dengan fitur Single Instance Lock
- Jika masih terjadi, tutup semua instance lalu buka ulang

---

## 📁 Struktur File

| File | Fungsi |
|---|---|
| `Web-TR-Gateway.exe` | Aplikasi utama (satu-satunya file yang perlu didistribusikan) |
| `config.json` | File konfigurasi (otomatis dibuat saat pertama kali dijalankan) |
| `sample_cameras.csv` | Contoh format CSV untuk import kamera (opsional) |

> [!CAUTION]
> **JANGAN** membagikan file `id_rsa` kepada siapapun! File tersebut adalah kunci SSH privat ke server VPS Anda.

---

*Web-TR Tunnel Gateway v1.1.0 — ZeroTier Edition*
