# 🤖 Indodax Trading Bot (Go) - Safe Mode

Aplikasi ini adalah bot trading otomatis yang ditulis dalam Go (Golang) yang dirancang untuk platform Indodax. Bot ini beroperasi menggunakan strategi *Safe Mode*, memprioritaskan manajemen risiko (Stop Loss) dan mengoptimalkan harga masuk (Split Order) berdasarkan analisis teknikal (EMA, MACD, RSI).

## ✨ Fitur Utama

* **Arsitektur Modular:** Menggunakan Dependency Injection (DI) yang rapi untuk memisahkan service (Indodax Core, Market, Account, Order Manager, Monitor).

* **Strategi Kombinasi Indikator:** Menggunakan sinyal yang divalidasi dari **EMA (5/10), MACD (12/26/9), dan RSI (6)**.

* **Split Order Limit:** Membagi modal masuk menjadi dua order limit (default **60%** agresif dan **40%** defensif) untuk mendapatkan harga rata-rata yang lebih baik (*cost averaging*).

* **Manajemen Risiko Otomatis:** Otomatis memasang order Take Profit (TP) dan Stop Loss (SL) segera setelah posisi beli tereksekusi.

* **Smart Exit (Cut Loss):** Mampu melakukan *Market Sell* segera (*Cut Loss*) jika sinyal berubah menjadi *Strong Sell* saat posisi sedang merugi.

* **Recovery & Persistence:** Menyimpan dan memuat kembali status posisi terakhir dari file CSV lokal (`position_<pair>.csv`) untuk ketahanan saat *restart*.

* **Dry Run Mode:** Mode simulasi yang aman untuk menguji logika tanpa mengeksekusi order sungguhan.

* **Cooldown Cerdas:** Menerapkan *cooldown* waktu (2 menit default) setelah buy/sell untuk mencegah *overtrading* dan *whipsaw*.

## ⚙️ Instalasi dan Penyiapan

### 1. Prasyarat

Pastikan Anda telah menginstal **Go (Golang)** versi 1.18 ke atas.

### 2. Clone Repositori

```
git clone <URL_REPOSITORI_ANDA>
cd safe-mode
```

### 3. Instal Dependencies

Bot ini menggunakan `godotenv` untuk konfigurasi.

```
go mod tidy
```

### 4. Konfigurasi API (.env)

Buat file bernama **`.env`** di root folder proyek Anda dan isi dengan kredensial Indodax API Anda.

```
# KREDENSIAL INDODAX
API_KEY="KEY_DARI_INDODAX"
API_SECRET="SECRET_DARI_INDODAX"

# MODE
# DRY_RUN=true: Hanya simulasi/logging, tidak ada eksekusi order.
# DRY_RUN=false: LIVE trading.
DRY_RUN=true 

# TELEGRAM (OPSIONAL - Hapus atau kosongkan jika tidak ingin notifikasi)
# TELEGRAM_BOT_TOKEN="TOKEN_BOT_ANDA"
# TELEGRAM_CHAT_ID="ID_CHAT_ANDA"
```

### 5. Konfigurasi Parameter Trading (`main.go`)

Semua strategi trading diatur dalam struct `monCfg` di file `main.go`. Anda **wajib** menyesuaikan nilai-nilai ini sesuai toleransi risiko Anda.

```
// main.go (dalam fungsi main)
monCfg := &monitor.Config{
    Pair:      "btc_idr",       // Pair: contoh "btc_idr"
    TPPercent: 7.0,             // Take Profit (Target Untung)
    SLPercent: 4.0,             // Stop Loss (Batas Kerugian)
    PollDelay: 5 * time.Second, // Interval polling API

    BuyCooldown:  2 * time.Minute, // Cooldown setelah beli
    SellCooldown: 2 * time.Minute, // Cooldown setelah jual

    // === Split Order Config ===
    UseSplitOrder: true,         // Aktifkan Split Order
    SplitA:        60,           // Persentase Modal untuk Limit Order A (Agresif)
    SplitB:        40,           // Persentase Modal untuk Limit Order B (Defensif)

    // ModalIDR: 0 = Gunakan saldo IDR real-time.
    // ModalIDR: > 0 = Gunakan modal tetap (misal: 1000000.0)
    ModalIDR: 0, 
    
    // Minimal modal per order (Indodax min. Rp 20.000)
    MinModal: 20000.0, 
}
```

## ▶️ Cara Menjalankan

Jalankan bot dari root folder proyek:

```
go run main.go
```

## 📊 Logika Pengambilan Keputusan

Logika bot berputar di antara dua status utama, dimonitor oleh `monitor/trading.go`:

### I. Status: NOT HOLDING (Mencari Entry/Beli)

1. **Sinyal Beli:** Entry dilakukan jika sinyal memenuhi:

   * **Strong Buy** (EMA/MACD/RSI selaras) **ATAU**

   * **Weak Buy** **DIPERKUAT** oleh kondisi *Safe Entry* (harga saat ini di bawah rata-rata 24 jam).

2. **Order Placement:** Menempatkan order **Limit Buy** menggunakan skema split order (misal 60% @ `HargaMarket * 0.999` dan 40% @ `HargaMarket * 0.997`).

3. **Order Management:** Jika order Limit Buy aktif, bot akan membatalkannya dan kembali ke siklus berikutnya jika sinyal pasar tiba-tiba berubah menjadi **Strong Sell** atau **Weak Sell** untuk menghindari *whipsaw*.

### II. Status: HOLDING (Mencari Exit/Jual)

1. **Penempatan TP/SL:** Order **Limit Sell** untuk TP (misal, 7.0% di atas harga beli) dan SL (misal, 4.0% di bawah harga beli) dipasang secara otomatis segera setelah posisi tereksekusi.

2. **Smart Exit (Cut Loss):** Ini adalah fitur kritis *Safe Mode*. Jika **sinyal Strong Sell** muncul **SAAT** posisi sedang **merugi (PnL < 0)**, bot akan **Market Sell** segera (Cut Loss) untuk membatasi kerugian, mengabaikan Limit SL yang sudah dipasang.

3. **Manajemen Order Jual:** Bot akan terus memantau order TP/SL yang terpasang dan siap membatalkannya jika ada kondisi sinyal yang mengharuskan *re-entry* atau *hold* yang lebih agresif.

## 🗂️ Struktur File Penting

| File | Keterangan | 
 | ----- | ----- | 
| `main.go` | **Entry Point.** Inisialisasi semua service dan menjalankan loop monitor. | 
| `config/config.go` | Memuat kredensial API dan mode Dry Run dari `.env`. | 
| `service/indodax/indodax.go` | Service inti, menangani HMAC signing dan koneksi API TAPI. | 
| `service/market/market.go` | Mengambil data pasar publik (Ticker, Histori). | 
| `service/account/account.go` | Mengambil info akun, saldo, dan order terbuka. | 
| `ordermanager/manager.go` | Mengelola eksekusi order (Limit Buy/Sell, Market Sell, Cancel). | 
| `monitor/trading.go` | **Logika Utama.** Berisi `handleNotHolding` dan `handleHolding`. | 
| `monitor/indicator.go` | Kalkulasi teknikal (EMA, MACD, RSI) dan fungsi sinyal. | 
| `helper/csv_helpers.go` | Fungsi untuk menyimpan dan memuat `Position` ke/dari CSV. |
