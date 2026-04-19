# Fixing WebRTC Stream in Easypanel

Untuk mengaktifkan stream WebRTC di Dashboard RTSP2go yang berjalan di dalam Easypanel, Anda wajib membuka pintu (port) UDP agar data video bisa mengalir langsung ke browser Anda.

## Langkah 1: Tambahkan Port UDP di Easypanel

Secara default, Easypanel hanya membuka port 80/443 (HTTP). WebRTC membutuhkan port **8555** dengan protokol **UDP**.

1. Masuk ke Dashboard **Easypanel**.
2. Pilih project dan aplikasi `web-tr`.
3. Buka tab **Network** atau **Ports**.
4. Tambahkan port baru:
   - **Host Port:** `8555`
   - **Container Port:** `8555`
   - **Protocol:** Pilih `UDP`
5. Jika ada opsi untuk TCP, tambahkan juga port `8555` dengan protocol `TCP` (opsional tapi disarankan).
6. Klik **Save** dan **Deploy** ulang aplikasi.

## Langkah 2: Pastikan Port Firewall VPS Terbuka

Jika Anda menggunakan Ubuntu dan mengaktifkan `ufw` (firewall), Anda harus memastikan port 8555 UDP tidak diblokir oleh sistem operasi VPS.

Jalankan perintah ini di terminal VPS Anda:
```bash
sudo ufw allow 8555/udp
sudo ufw allow 8555/tcp
```

## Kenapa ini perlu?
Signaling (negosiasi) WebRTC berjalan lewat HTTP (port 80/443), itulah kenapa dashboard Anda bisa terbuka. Tapi, pengiriman data video "asli" WebRTC dikirimkan lewat jalur cepat (UDP). Tanpa membuka port UDP 8555, browser Anda tidak akan pernah menerima data video meskipun koneksi sudah terjalin.

---

> [!TIP]
> Saya sudah memperbarui kode di backend agar secara otomatis menambahkan konfigurasi WebRTC yang optimal jika file konfigurasi kosong. Anda cukup melakukan **Deploy** ulang di Easypanel setelah mengunggah perubahan terbaru saya ke Github.
