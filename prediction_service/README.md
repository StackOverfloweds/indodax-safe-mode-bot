
# 🧠 Bot Trading Safe-Mode dengan Deep Learning (DL) Cut Loss

Aplikasi ini adalah bot trading kripto Go yang diperkuat dengan layanan prediksi Deep Learning (DL) Python/TensorFlow. Layanan DL berfungsi sebagai sistem peringatan dini (Cut Loss) berbasis probabilitas risiko.

## ⚙️ Arsitektur Sistem

Sistem ini terbagi menjadi dua komponen utama yang berjalan secara independen dan berkomunikasi melalui HTTP:

1. **Bot Go (Safe-Mode Core):**

   * Mengambil data pasar secara *real-time* (polling).

   * Mengelola posisi, penempatan Limit Buy, dan eksekusi Take Profit (TP) & Stop Loss (SL) Tradisional (berbasis persentase).

   * **Peran DL:** Mengirim 100 *time steps* data historis ke API DL Python pada setiap siklus.

   * **Keputusan Cut Loss:** Jika probabilitas DL (`prob_cutloss`) melebihi ambang batas (`DLProbThreshold`) *dan* posisi sedang mengalami kerugian, bot akan memicu Market Sell (Cut Loss).

2. **Layanan Prediksi Python (dl_models.py & prediction_api.py):**

   * **`dl_models.py`:** Berisi logika pelatihan model Ensemble (CNN-LSTM & GRU) dan fungsi penyimpanan/pemuatan model.

   * **`prediction_api.py`:** Membungkus model DL yang sudah dilatih ke dalam server web API Flask yang ringan.

   * **Input:** Data *time series* \[100x5\] (fitur: Price Change, PnL Ratio, Volatility, RSI, Volume).

   * **Output:** Probabilitas risiko Cut Loss (0.0 - 1.0) dari Ensemble model.
   


## 🛠️ Persiapan Lingkungan

Anda memerlukan **Go** (untuk Bot Trading) dan **Python** (untuk Layanan DL).

### 1. Persiapan Lingkungan Python (Prediction Service)

Pastikan Anda berada di direktori `prediction_service/` dan telah mengaktifkan `venv`.

1. **Instal Dependensi:**

   ```bash
   pip install -r requirements.txt
   
   ```

2. **Latih Model (Hanya sekali):**
   Jalankan skrip training untuk membuat file model `.h5` yang diperlukan.

   ```bash
   python dl_models.py
   
   ```

3. **Jalankan API DL (Wajib berjalan 24/7):**
   API ini harus berjalan di *background* sebelum Anda menjalankan bot Go. API ini akan *listen* pada port `8000`.

   ```bash
   python prediction_api.py
   
   ```

   Output: `* Running on http://127.0.0.1:8000 (Press CTRL+C to quit)`

### 2. Persiapan Lingkungan Go (Safe-Mode Core)

Pastikan Anda telah menginstal Go dan memiliki file `.env` yang dikonfigurasi.

1. **Konfigurasi `.env`:**
   Tambahkan variabel lingkungan untuk API DL.

   ```bash
   # Konfigurasi Indodax
   API_KEY="your_indodax_api_key"
   API_SECRET="your_indodax_secret"
   DRY_RUN="false" # Atur ke "true" untuk simulasi
   
   # Konfigurasi Layanan DL
   # Pastikan ini sama dengan alamat tempat prediction_api.py berjalan
   DL_SERVICE_URL="[http://127.0.0.1:8000](http://127.0.0.1:8000)"
   
   ```

2. **Jalankan Bot Go:**

   ```bash
   go run main.go
   
   ```

## 🚀 Alur Kerja Prediksi Cut Loss

Setiap siklus polling di Bot Go akan mengikuti langkah-langkah berikut:

1. **Pengambilan Data:** `monitor.Monitor` mengambil harga terbaru (`lastPrice`) dan memperbarui `priceBuffer`.

2. **Pengecekan Posisi:** Jika ada posisi aktif (`pos.Active == true`), hitung PnL saat ini.

3. **Format Data:** Fungsi `dl.FormatDataForDL` (di `service/dl/dl_service.go`) mengambil 100 data historis terakhir, melakukan *feature engineering* (5 fitur), dan menyiapkannya menjadi matriks \[100x5\].

4. **Panggilan API:** Bot Go mengirimkan matriks \[100x5\] ke `POST http://127.0.0.1:8000/predict_cutloss`.

5. **Eksekusi DL:** Server Python menjalankan data melalui model Ensemble CNN-LSTM & GRU, menghitung probabilitas rata-rata (`prob_cutloss`), dan mengembalikan JSON:

   ```json
   {
     "prob_cutloss": 0.825,
     "source_preds": {
       "gru": 0.79,
       "cnn_lstm": 0.86
     }
   }
   
   ```
