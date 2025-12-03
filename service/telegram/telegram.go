package telegram

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

type Client struct {
	Token  string
	ChatID string
}

func NewClient(token string, chatID string) *Client {
	return &Client{
		Token:  token,
		ChatID: chatID,
	}
}

type sendMessageReq struct {
	ChatID string `json:"chat_id"`
	Text   string `json:"text"`
}

func (c *Client) Send(msg string) error {
	if c.Token == "" || c.ChatID == "" {
		// Jika bot tidak dikonfigurasi, jangan error—diam saja.
		return nil
	}

	url := fmt.Sprintf("https://api.telegram.org/bot%s/sendMessage", c.Token)

	body, _ := json.Marshal(sendMessageReq{
		ChatID: c.ChatID,
		Text:   msg,
	})

	req, err := http.NewRequest("POST", url, bytes.NewBuffer(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")

	client := &http.Client{Timeout: 10 * time.Second}
	_, err = client.Do(req)

	return err
}
