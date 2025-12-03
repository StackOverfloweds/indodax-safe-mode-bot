import numpy as np
import tensorflow as tf
from tensorflow.keras.models import Model
from tensorflow.keras.layers import Input, Conv1D, LSTM, GRU, Dense, concatenate
from sklearn.model_selection import train_test_split
import os
import logging

# Konfigurasi
SEQUENCE_LENGTH = 100  # Jumlah langkah waktu (time steps)
N_FEATURES = 5         # Jumlah fitur
MODEL_PATH = 'cutloss_ensemble_model.h5'

# Atur logging
logging.basicConfig(level=logging.INFO, format='%(asctime)s - %(levelname)s - %(message)s')

def create_dummy_data(samples=5000):
    """
    Membuat data dummy untuk simulasi training.
    Gantilah bagian ini dengan data time series riil Anda yang sudah di-feature engineered.
    """
    logging.info("Membuat data time series dummy untuk pelatihan...")
    
    # X_data: [samples, sequence_length, n_features] -> [5000, 100, 5]
    X_data = np.random.randn(samples, SEQUENCE_LENGTH, N_FEATURES).astype(np.float32)
    
    # Y_data: Label target [samples] -> [5000]
    y_data = np.zeros(samples, dtype=np.int32)
    high_risk_indices = np.random.choice(samples, int(samples * 0.2), replace=False)
    y_data[high_risk_indices] = 1
    
    # Modifikasi data input sedikit agar model punya pola untuk dipelajari
    # Catatan: Dalam data riil, pastikan semua fitur diskalakan (e.g., MinMax atau StandardScaler)
    X_data[high_risk_indices, :, 1] = -np.abs(X_data[high_risk_indices, :, 1]) - 0.5  # PnL Ratio negatif (rugi)
    X_data[high_risk_indices, :, 2] = np.abs(X_data[high_risk_indices, :, 2]) + 1.0   # Volatility tinggi
    
    logging.info(f"Data Dummy dibuat: X shape {X_data.shape}, Y shape {y_data.shape}")
    return X_data, y_data

def create_ensemble_model():
    """
    Membangun model Ensemble Deep Learning (CNN-LSTM & GRU)
    """
    input_layer = Input(shape=(SEQUENCE_LENGTH, N_FEATURES), name='input_features')

    # Path 1: CNN -> LSTM (Mengekstrak fitur dan memproses urutan)
    # Gunakan padding='same' agar dimensi waktu tetap, atau GlobalMaxPooling jika ingin mengecilkan.
    # Disini kita tidak menggunakan GlobalMaxPooling1D agar LSTM menerima urutan
    conv_layer = Conv1D(filters=64, kernel_size=5, activation='relu', padding='causal')(input_layer)
    # Tambahkan Dropout untuk regulasi
    lstm_layer = LSTM(64, activation='tanh', name='lstm_output', dropout=0.2, recurrent_dropout=0.2)(conv_layer) 
    
    # Path 2: GRU (Langsung memproses urutan)
    gru_layer = GRU(64, activation='tanh', name='gru_output', dropout=0.2, recurrent_dropout=0.2)(input_layer)

    # Gabungkan hasil kedua path
    merged = concatenate([lstm_layer, gru_layer], name='merged_features')

    # Fully Connected Layers
    dense_1 = Dense(32, activation='relu')(merged)
    # Output sigmoid untuk probabilitas biner (0-1)
    output_layer = Dense(1, activation='sigmoid', name='cutloss_prob')(dense_1)

    model = Model(inputs=input_layer, outputs=output_layer, name='Ensemble_CutLoss_Model')
    
    # Kompilasi model: Binary Crossentropy untuk masalah klasifikasi biner
    model.compile(optimizer='adam', 
                  loss='binary_crossentropy', 
                  metrics=['accuracy', tf.keras.metrics.AUC(name='auc')])
    
    return model

def train_and_save_model():
    """
    Melatih model dan menyimpannya ke file .h5
    """
    X, y = create_dummy_data()
    
    # Split data untuk training dan testing
    X_train, X_test, y_train, y_test = train_test_split(X, y, test_size=0.2, random_state=42, stratify=y)
    
    model = create_ensemble_model()
    model.summary()
    
    logging.info("\nMemulai pelatihan model...")
    
    # Latih model
    history = model.fit(X_train, y_train, 
                        epochs=30, # Naikkan epochs sedikit
                        batch_size=64, # Naikkan batch size sedikit
                        validation_data=(X_test, y_test),
                        verbose=1)

    # Evaluasi
    loss, acc, auc = model.evaluate(X_test, y_test, verbose=0)
    logging.info(f"\n✅ Pelatihan selesai. Akurasi Test: {acc*100:.2f}%, AUC: {auc:.4f}")
    
    # Simpan model
    try:
        # Hapus file lama jika ada untuk menghindari warning/error
        if os.path.exists(MODEL_PATH):
            os.remove(MODEL_PATH)
        model.save(MODEL_PATH)
        logging.info(f"✅ Model Ensemble berhasil disimpan ke: {MODEL_PATH}")
    except Exception as e:
        logging.error(f"❌ Gagal menyimpan model: {e}")

if __name__ == '__main__':
    # Konfigurasi GPU (Best Practice)
    gpus = tf.config.experimental.list_physical_devices('GPU')
    if gpus:
        try:
            # Memastikan TensorFlow hanya mengambil memori saat diperlukan
            for gpu in gpus:
                tf.config.experimental.set_memory_growth(gpu, True)
        except RuntimeError as e:
            logging.error(e)
            
    train_and_save_model()