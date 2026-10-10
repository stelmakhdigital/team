package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"
)

// Client — HTTP-клиент к демону (teamctl — клиент REST, не обход).
type Client struct {
	server string
	apiKey string
	hc     *http.Client
}

// resolveServer — --server > env TEAMCTL_SERVER > default.
func resolveServer(flagVal string) string {
	s := strings.TrimRight(flagVal, "/")
	if s == "" {
		s = strings.TrimRight(os.Getenv("TEAMCTL_SERVER"), "/")
	}
	if s == "" {
		s = "http://localhost:8080"
	}
	return s
}

// resolveAPIKey — --api-key > env TEAMCTL_API_KEY.
func resolveAPIKey(flagVal string) string {
	if flagVal != "" {
		return flagVal
	}
	return os.Getenv("TEAMCTL_API_KEY")
}

func newClient(server, apiKey string) *Client {
	return &Client{
		server: resolveServer(server),
		apiKey: resolveAPIKey(apiKey),
		hc:     &http.Client{Timeout: 30 * time.Second},
	}
}

// Do — метод+путь+body → (status, raw). out — куда декодировать JSON (nil = ignore).
// Ошибка сети/транспортной — error; HTTP-статус (включая 4xx/5xx) — НЕ error
// (клиент сам решает по статусу + error envelope).
func (c *Client) Do(ctx context.Context, method, path string, body any, out any) (int, []byte, error) {
	var rdr io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return 0, nil, fmt.Errorf("marshal body: %w", err)
		}
		rdr = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.server+path, rdr)
	if err != nil {
		return 0, nil, fmt.Errorf("new request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	if c.apiKey != "" {
		req.Header.Set("X-API-Key", c.apiKey)
	}
	resp, err := c.hc.Do(req)
	if err != nil {
		return 0, nil, fmt.Errorf("request %s %s: %w", method, path, err)
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return resp.StatusCode, nil, fmt.Errorf("read body: %w", err)
	}
	if out != nil && len(raw) > 0 {
		_ = json.Unmarshal(raw, out) // парсинг ошибок не критичен (статус решит)
	}
	return resp.StatusCode, raw, nil
}
