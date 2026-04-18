# 📋 Panduan Format URL RTSP (RTSP2go)

RTSP (Real Time Streaming Protocol) adalah standar yang digunakan oleh hampir semua kamera IP untuk mengirimkan data video. Setiap merk kamera memiliki format URL yang sedikit berbeda.

---

## 🏗️ Struktur Dasar
Secara umum, format URL RTSP adalah:
`rtsp://[username]:[password]@[IP_Address]:[Port]/[Path]`

- **Protocol**: `rtsp://`
- **Credentials**: Username dan password kamera (opsional jika akses publik dibuka).
- **IP Address**: Alamat IP lokal kamera (misal: `192.168.1.100`) atau domain.
- **Port**: Default adalah **554**.
- **Path**: Jalur spesifik stream (misal: `/live`, `/h264`, `/Streaming/Channels/101`).

---

## 📸 Contoh Berdasarkan Merk Kamera

### 1. Generic / Umum
Banyak kamera OEM menggunakan format standar ini:
- `rtsp://admin:password@192.168.1.100:554/h264/ch1/main/av_stream`
- `rtsp://192.168.1.100:554/live`

### 2. Dahua
Dahua biasanya menggunakan parameter channel dan subtype:
- `rtsp://admin:admin@10.7.6.67:554/cam/realmonitor?channel=1&subtype=1`
- `rtsp://admin:admin@10.7.6.67/Streaming/Channels/1`

### 3. Hikvision
Hikvision mendefinisikan channel dalam angka 3 digit (101 untuk channel 1 main stream):
- `rtsp://admin:12345@192.168.1.210:554/Streaming/Channels/101`
- `rtsp://admin:12345@192.168.1.210:554/h264/ch1/main/av_stream`

### 4. Panasonic & Samsung
- **Panasonic**: `rtsp://192.168.0.253/MediaInput/h264`
- **Samsung**: `rtsp://192.168.1.200/h264/media.smp`

### 5. Test Stream (Wowza)
- `rtsp://716f898c7b71.entrypoint.cloud.wowza.com:1935/app-8F9K44lJ/304679fe_stream2`

---

## 💡 Tips Penting
1. **Codec H.264**: Pastikan pengaturan kamera Anda menggunakan codec **H.264**. Codec H.265 mungkin memerlukan hardware acceleration tambahan atau tidak didukung oleh semua browser lawas.
2. **Port Forwarding**: Jika Anda menggunakan **RTSP2go Gateway**, Anda **TIDAK PERLU** melakukan port forwarding di router. Gateway akan menangani koneksi secara aman.
3. **Kredensial**: Sangat disarankan untuk tidak menggunakan password default (seperti `admin/admin`) untuk alasan keamanan.

---

*Dokumentasi ini dibuat untuk membantu pengguna RTSP2go menghubungkan kamera mereka dengan mudah.*
