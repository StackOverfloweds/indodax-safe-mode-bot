package indodax

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha512"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"

	"safe-mode/config" // SESUAIKAN DENGAN NAMA MODULE ANDA
)

// TAPIResponse adalah struktur respon umum untuk Indodax TAPI (Private)
type TAPIResponse struct {
	Success int             `json:"success"`
	Return  json.RawMessage `json:"return"`
	Error   string          `json:"error"`
}

// Service adalah struktur inti Indodax yang di-embed ke sub-service lain.
type Service struct {
	conf   *config.Config
	client *http.Client
}

// NewService adalah konstruktor inti
func NewService(cfg *config.Config) *Service {
	return &Service{
		conf: cfg,
		client: &http.Client{
			Timeout: 10 * time.Second,
		},
	}
}

// sign membuat HMAC SHA512 dari payload
func (s *Service) sign(payload string) string {
	mac := hmac.New(sha512.New, []byte(s.conf.APISecret))
	mac.Write([]byte(payload))
	return hex.EncodeToString(mac.Sum(nil))
}

// PrivatePost menangani permintaan POST ke TAPI
func (s *Service) PrivatePost(ctx context.Context, params url.Values) (*TAPIResponse, error) {
	if s.conf.APIKey == "" || s.conf.APISecret == "" {
		return nil, errors.New("API key/secret missing")
	}

	params.Set("nonce", fmt.Sprintf("%d", time.Now().UnixMilli()))

	payload := params.Encode()
	signature := s.sign(payload)

	req, err := http.NewRequestWithContext(ctx, "POST", s.conf.TapiURL, bytes.NewBufferString(payload))
	if err != nil {
		return nil, err
	}

	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Key", s.conf.APIKey)
	req.Header.Set("Sign", signature)

	resp, err := s.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	bodyBytes, _ := io.ReadAll(resp.Body)

	var apiResp TAPIResponse
	if err := json.Unmarshal(bodyBytes, &apiResp); err != nil {
		return nil, fmt.Errorf("invalid json response: %w (raw: %s)", err, string(bodyBytes))
	}

	if apiResp.Success != 1 {
		if apiResp.Error == "" {
			apiResp.Error = "unknown error"
		}
		return &apiResp, errors.New(apiResp.Error)
	}

	return &apiResp, nil
}

// Getter untuk konfigurasi BaseURL dan Client (Berguna untuk sub-package)
func (s *Service) BaseURL() string {
	return s.conf.BaseURL
}

func (s *Service) Client() *http.Client {
	return s.client
}

func (s *Service) DryRun() bool {
	return s.conf.DryRun
}
