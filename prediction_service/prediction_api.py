import os
import logging
import time
import random
import numpy as np
import tensorflow as tf
from tensorflow.keras.models import Model
from tensorflow.keras.layers import Input, Conv1D, LSTM, GRU, Dense, concatenate, Dropout
from sklearn.model_selection import train_test_split
from flask import Flask, request, jsonify

# ==============================================================================
# KONFIGURASI GLOBAL
# ==============================================================================
# Catatan: Variabel PORT tetap 5000 secara default, diasumsikan dikonfigurasi di Go
PORT = int(os.environ.get('PORT', 5000))
SEQUENCE_LENGTH = 30  # Jumlah langkah waktu (time steps) dari data historis Go
N_FEATURES = 5         # Jumlah fitur yang dikirim (e.g., [Open, High, Low, Close, Volume])
MODEL_PATH = 'cutloss_ensemble_model.h5'

app = Flask(__name__)
model = None # Variabel global untuk menampung model TensorFlow

# Konfigurasi Logging
# Menambahkan level DEBUG untuk output verbose TensorFlow
logging.basicConfig(level=logging.INFO, format='%(asctime)s - %(levelname)s - %(message)s')

# ==============================================================================
# FUNGSI PELATIHAN MODEL (Trainer Logic)
# ==============================================================================

def create_dummy_data(samples=5000):
    """
    Membuat data dummy untuk simulasi training.
    """
    logging.info("Membuat data time series dummy untuk pelatihan...")
    
    # X_data: [samples, sequence_length, n_features] -> [5000, 30, 5]
    X_data = np.random.randn(samples, SEQUENCE_LENGTH, N_FEATURES).astype(np.float32)
    
    # Y_data: Label target [samples] (0: Aman, 1: Risiko Cut Loss)
    y_data = np.zeros(samples, dtype=np.int32)
    
    # Simulasikan 20% data sebagai kasus risiko tinggi (label 1)
    high_risk_indices = np.random.choice(samples, int(samples * 0.2), replace=False)
    y_data[high_risk_indices] = 1
    
    # Modifikasi data input untuk simulasi pola 'risiko tinggi'
    X_data[high_risk_indices, :, 1] = -np.abs(X_data[high_risk_indices, :, 1]) - 0.5
    X_data[high_risk_indices, :, 2] = np.abs(X_data[high_risk_indices, :, 2]) + 1.0
    
    logging.info(f"Data Dummy dibuat: X shape {X_data.shape}, Y shape {y_data.shape}")
    return X_data, y_data

def create_ensemble_model():
    """
    Membangun model Ensemble Deep Learning (CNN-LSTM & GRU) untuk klasifikasi biner.
    """
    input_layer = Input(shape=(SEQUENCE_LENGTH, N_FEATURES), name='input_features')

    # Path 1: CNN -> LSTM
    conv_layer = Conv1D(filters=64, kernel_size=5, activation='relu', padding='causal')(input_layer)
    lstm_layer = LSTM(64, activation='tanh', name='lstm_output', dropout=0.2, recurrent_dropout=0.2)(conv_layer) 
    
    # Path 2: GRU
    gru_layer = GRU(64, activation='tanh', name='gru_output', dropout=0.2, recurrent_dropout=0.2)(input_layer)

    # Gabungkan hasil dari kedua path
    merged = concatenate([lstm_layer, gru_layer], name='merged_features')

    # Fully Connected Layers
    dense_1 = Dense(32, activation='relu')(merged)
    output_layer = Dense(1, activation='sigmoid', name='cutloss_prob')(dense_1) # Output: Probabilitas (0-1)

    model = Model(inputs=input_layer, outputs=output_layer, name='Ensemble_CutLoss_Model')
    
    # Kompilasi model
    model.compile(optimizer='adam', 
                  loss='binary_crossentropy', 
                  metrics=['accuracy', tf.keras.metrics.AUC(name='auc')])
    
    return model

@app.route('/train', methods=['POST'])
def train_and_save_model_api():
    """
    Endpoint untuk memicu pelatihan model secara manual (opsional).
    """
    logging.info("Memulai proses pelatihan model yang dipicu via API...")
    try:
        train_and_save_model()
        # Muat ulang model setelah dilatih
        if load_model():
            return jsonify({"status": "success", "message": "Pelatihan dan pemuatan model berhasil."}), 200
        else:
            return jsonify({"status": "error", "message": "Pelatihan selesai, tetapi gagal memuat model baru."}), 500
    except Exception as e:
        logging.error(f"❌ Gagal melatih model: {e}")
        return jsonify({"status": "error", "message": f"Kesalahan pelatihan: {str(e)}"}), 500

def train_and_save_model():
    """
    Melatih model dan menyimpannya ke file .h5 (Dipanggil secara internal atau via API)
    """
    X, y = create_dummy_data()
    
    # Split data untuk training dan testing
    X_train, X_test, y_train, y_test = train_test_split(X, y, test_size=0.2, random_state=42, stratify=y)
    
    model_to_train = create_ensemble_model()
    
    logging.info(f"\nMemulai pelatihan model (Sequence Length={SEQUENCE_LENGTH}, Epochs=30)...")
    
    # Latih model
    model_to_train.fit(X_train, y_train, 
                        epochs=30, 
                        batch_size=64, 
                        validation_data=(X_test, y_test),
                        verbose=1) # PERUBAHAN: verbose=1 untuk menampilkan log setiap epoch

    # Evaluasi
    loss, acc, auc = model_to_train.evaluate(X_test, y_test, verbose=0)
    logging.info(f"✅ Pelatihan selesai. Akurasi Test: {acc*100:.2f}%, AUC: {auc:.4f}")
    
    # Simpan model
    try:
        # Menghapus file lama jika ada
        if os.path.exists(MODEL_PATH):
            os.remove(MODEL_PATH)
        model_to_train.save(MODEL_PATH)
        logging.info(f"✅ Model Ensemble berhasil disimpan ke: {MODEL_PATH}")
    except Exception as e:
        logging.error(f"❌ Gagal menyimpan model: {e}")

# ==============================================================================
# FUNGSI PENYAJIAN MODEL (API Serving Logic)
# ==============================================================================

def load_model():
    """Memuat model Deep Learning yang sudah dilatih."""
    global model
    if not os.path.exists(MODEL_PATH):
        logging.warning(f"File model '{MODEL_PATH}' belum ada.")
        return False
    try:
        model = tf.keras.models.load_model(MODEL_PATH, compile=False) 
        logging.info(f"✅ Model Deep Learning berhasil dimuat dari: {MODEL_PATH}")
        
        # Warm-up model dengan input dummy
        dummy_input = np.zeros((1, SEQUENCE_LENGTH, N_FEATURES), dtype=np.float32)
        _ = model.predict(dummy_input, verbose=0)
        logging.info("✅ Model siap (Warm-up selesai).")
        return True
    except Exception as e:
        logging.error(f"❌ Gagal memuat model: {e}")
        return False

@app.route('/', methods=['GET'])
def health_check():
    """Endpoint untuk health check."""
    global model
    return jsonify({
        "status": "online",
        "service": "DL Cut Loss Predictor",
        "model_loaded": model is not None,
        "model_name": "Ensemble_CNN_LSTM_GRU",
        "sequence_length": SEQUENCE_LENGTH,
        "features": N_FEATURES
    }), 200

@app.route('/predict', methods=['POST'])
def predict_cutloss():
    """
    Endpoint utama untuk menerima data time series (dari bot Go) dan 
    mengembalikan probabilitas cut loss.
    """
    start_time = time.time()
    
    if model is None:
        logging.error("Model tidak dimuat saat /predict dipanggil. Mengembalikan 503.")
        return jsonify({"error": "Model belum dimuat. Periksa log server."}), 503 

    try:
        # LOGGING PENGAKUAN PERMINTAAN KLIEN
        logging.info("➡️ Menerima permintaan prediksi dari Klien Go...")
        
        # 1. Ambil data dari request JSON
        data = request.get_json(force=True) 
        if data is None or 'input_data' not in data:
            logging.warning("Permintaan /predict tanpa data JSON yang valid.")
            return jsonify({"error": "Payload JSON harus berisi kunci 'input_data' (list of lists)."}), 400

        input_data_list = data['input_data']
        # Logging bentuk input
        logging.info(f"   Ukuran data input: {len(input_data_list)}x{len(input_data_list[0]) if input_data_list and len(input_data_list)>0 else '0'}")

        # Input data diharapkan berupa list 2D (time series) [30x5]
        input_data = np.array(input_data_list, dtype=np.float32)

        # 2. Validasi dan Konversi Shape Input
        if input_data.ndim == 2 and input_data.shape == (SEQUENCE_LENGTH, N_FEATURES):
            X_input = input_data[np.newaxis, ...] 
        else:
             logging.error(f"Shape input tidak valid. Diharapkan: ({SEQUENCE_LENGTH}, {N_FEATURES}), Diterima: {str(input_data.shape)}")
             return jsonify({
                "error": "Shape input_data tidak valid.", 
                "expected_shape": f"({SEQUENCE_LENGTH}, {N_FEATURES})", 
                "received_shape": str(input_data.shape)
            }), 400

        # 3. Prediksi
        predictions = model.predict(X_input, verbose=0)
        prob_cutloss = float(predictions[0][0])
        
        # 4. Format Output
        response = {
            "prob_cutloss": prob_cutloss, 
            "decision": "CUT_LOSS" if prob_cutloss > 0.5 else "HOLD",
            "elapsed_ms": round((time.time() - start_time) * 1000, 2)
        }
        
        # LOGGING PENGIRIMAN RESPON KLIEN
        logging.info(f"✅ Prediksi berhasil dalam {response['elapsed_ms']:.2f} ms. Prob: {prob_cutloss:.4f} ({response['decision']}). Mengirim respons ke klien.")
        return jsonify(response), 200

    except Exception as e:
        logging.error(f"❌ Error saat memproses prediksi: {e}")
        return jsonify({"error": f"Terjadi kesalahan internal server: {str(e)}"}), 500

# ==============================================================================
# MAIN ENTRY POINT
# ==============================================================================

if __name__ == '__main__':
    logging.info("Memulai Layanan Prediksi Deep Learning Terpadu...")
    
    # 1. Coba muat model yang sudah ada
    model_ready = load_model()

    # 2. Jika model belum ada, latih dan simpan secara otomatis
    if not model_ready:
        logging.warning(f"File model '{MODEL_PATH}' tidak ditemukan. Memulai proses pelatihan otomatis...")
        try:
            train_and_save_model() 
            # Muat ulang model setelah dilatih
            model_ready = load_model() 
        except Exception as e:
            logging.error(f"❌ Gagal melatih model: {e}")
            model_ready = False
    
    # 3. Jalankan server hanya jika model siap
    if model_ready:
        # LOGGING KONFIRMASI KONEKSI SERVER
        logging.info("====================================================================")
        logging.info(f"🚀 SERVER SIAP! DL Prediction Service berjalan di http://127.0.0.1:{PORT}/")
        logging.info("Status: TERSAMBUNG dan menanti permintaan /predict dari klien Go.")
        logging.info("====================================================================")
        
        app.run(host='127.0.0.1', port=PORT, debug=False)
    else:
        logging.critical("❌ Model tidak siap. Server tidak dapat dimulai.")