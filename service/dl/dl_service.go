package dl

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"safe-mode/config"
)

// --- Struct Komunikasi API ---

// DLRequest adalah payload yang dikirim ke API Python
type DLRequest struct {
	InputData [][]float64 `json:"input_data"`
}

// DLResponse adalah payload yang diterima dari API Python untuk prediksi 5-kelas
// Ini adalah respons lengkap dari layanan DL.
type DLResponse struct {
	BestAction    string             `json:"best_action"`   // Contoh: "BUY", "TAKE_PROFIT", "HOLD"
	BestProb      float64            `json:"best_prob"`     // Probabilitas aksi terbaik
	Probabilities map[string]float64 `json:"probabilities"` // Semua probabilitas (e.g., {"BUY": 0.8, "SELL": 0.1, ...})
	Error         string             `json:"error"`         // Pesan error dari API Python jika ada
}

// Service menangani komunikasi dengan layanan prediksi Deep Learning
type Service struct {
	client *http.Client
	config *config.Config
	url    string // URL lengkap ke endpoint /predict_action
}

// NewService adalah konstruktor untuk DL Service
func NewService(cfg *config.Config) *Service {
	// Cek jika URL layanan DL tidak dikonfigurasi, bot akan berjalan tanpa fitur DL
	if cfg.DLServiceURL == "" {
		return &Service{
			config: cfg,
			url:    "",
		}
	}
	// Endpoint diperbarui dari /predict_cutloss menjadi /predict_action
	return &Service{
		config: cfg,
		client: &http.Client{
			Timeout: 5 * time.Second, // Timeout singkat untuk prediksi DL
		},
		url: fmt.Sprintf("%s/predict_action", cfg.DLServiceURL),
	}
}

// predictActionRaw mengirim data time series ke API Python dan mendapatkan respons 5-kelas
func (s *Service) predictActionRaw(ctx context.Context, inputData [][]float64) (*DLResponse, error) {
	if s.url == "" {
		return nil, fmt.Errorf("DL_SERVICE_URL tidak dikonfigurasi. Fitur DL dinonaktifkan")
	}

	// 1. Siapkan Request Payload
	reqBody := DLRequest{InputData: inputData}
	jsonBody, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("gagal marshal request: %w", err)
	}

	// 2. Buat dan Kirim Request
	req, err := http.NewRequestWithContext(ctx, "POST", s.url, bytes.NewBuffer(jsonBody))
	if err != nil {
		return nil, fmt.Errorf("gagal membuat request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := s.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("gagal menghubungi DL API (%s): %w", s.url, err)
	}
	defer resp.Body.Close()

	// 3. Proses Response
	var dlResp DLResponse
	if err := json.NewDecoder(resp.Body).Decode(&dlResp); err != nil {
		return nil, fmt.Errorf("gagal decode response JSON: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return &dlResp, fmt.Errorf("DL API mengembalikan status %d: %s", resp.StatusCode, dlResp.Error)
	}

	if dlResp.Error != "" {
		return &dlResp, fmt.Errorf("DL API mengembalikan error: %s", dlResp.Error)
	}

	return &dlResp, nil
}

// PredictAction adalah fungsi yang dipanggil oleh monitor.go
// Fungsi ini mengelola komunikasi API dan mengembalikan hasil 5-kelas.
// Monitor.go sekarang mengharapkan objek DLResponse ini.
func (s *Service) PredictAction(inputData [][]float64) (*DLResponse, error) {
	// Jika DL service dinonaktifkan, kembalikan aksi default (HOLD)
	if s.url == "" {
		return &DLResponse{
			BestAction:    "HOLD",
			BestProb:      1.0,
			Probabilities: map[string]float64{"HOLD": 1.0},
		}, nil
	}

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	resp, err := s.predictActionRaw(ctx, inputData)
	if err != nil {
		return nil, fmt.Errorf("DL prediction error: %w", err)
	}

	return resp, nil
}
