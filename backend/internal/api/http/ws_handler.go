package httpapi

import (
	"encoding/json"
	"net/http"
	"sync"
	"time"

	"github.com/gorilla/websocket"

	"daemon/internal/service"
)

// WS /ws — real-time события (контракт 20 §WebSocket).
//
// Протокол:
//   - client → server: {"type":"subscribe","channels":["team:1","task:5"]}
//     (повторный subscribe заменяет набор каналов)
//   - server → client: {"type":"<event>","data":{...},"timestamp":"RFC3339"} —
//     только для событий, канал которых среди подписанных.
//
// Auth: если API-ключи включены — ?api_key= (браузерный WebSocket не может
// шлать заголовки) либо X-API-Key/Authorization (как для REST; authMW).
//
// Отключение медленных клиентов: write-таймаут 10с + ping каждые 30с.
const (
	wsReadLimit  = 4 << 10
	wsWriteWait  = 10 * time.Second
	wsPongWait   = 60 * time.Second
	wsPingPeriod = 30 * time.Second
)

var wsUpgrader = websocket.Upgrader{
	ReadBufferSize:  1024,
	WriteBufferSize: 1024,
	// локальный инструмент: Origin не проверяем (frontend dev — другой порт).
	CheckOrigin: func(r *http.Request) bool { return true },
}

type wsClient struct {
	h    *handlers
	conn *websocket.Conn

	mu       sync.Mutex // write + channels
	channels map[string]struct{}
}

// HandleWS — GET /ws (upgrade → read/write pump'ы).
func (h *handlers) HandleWS(w http.ResponseWriter, r *http.Request) {
	if h.events == nil {
		writeError(w, r, &service.AppError{Code: "internal", Status: 503, Message: "event bus unavailable"})
		return
	}
	// authMW проверяет заголовки; для браузера — ?api_key= (см. middleware).
	conn, err := wsUpgrader.Upgrade(w, r, nil)
	if err != nil {
		return // upgrade ответил сам (400)
	}
	c := &wsClient{h: h, conn: conn, channels: map[string]struct{}{}}
	go c.writePump()
	c.readPump()
}

func (c *wsClient) readPump() {
	defer c.conn.Close()
	c.conn.SetReadLimit(wsReadLimit)
	_ = c.conn.SetReadDeadline(time.Now().Add(wsPongWait))
	c.conn.SetPongHandler(func(string) error {
		return c.conn.SetReadDeadline(time.Now().Add(wsPongWait))
	})
	for {
		_, raw, err := c.conn.ReadMessage()
		if err != nil {
			return
		}
		var req struct {
			Type     string   `json:"type"`
			Channels []string `json:"channels"`
		}
		if err := json.Unmarshal(raw, &req); err != nil || req.Type != "subscribe" {
			continue // мусор/не-known-тип — игнорируем
		}
		set := make(map[string]struct{}, len(req.Channels))
		for _, ch := range req.Channels {
			if ch != "" {
				set[ch] = struct{}{}
			}
		}
		c.mu.Lock()
		c.channels = set
		c.mu.Unlock()
	}
}

// writePump — шлёт события EventBus, отфильтрованные по подписанным каналам.
func (c *wsClient) writePump() {
	events, unsub := c.h.events.Subscribe()
	defer unsub()

	ping := time.NewTicker(wsPingPeriod)
	defer ping.Stop()

	for {
		select {
		case e, ok := <-events:
			if !ok {
				return
			}
			if !c.matchChannels(e.Channels) {
				continue
			}
			c.mu.Lock()
			c.conn.SetWriteDeadline(time.Now().Add(wsWriteWait))
			err := c.conn.WriteJSON(e)
			c.mu.Unlock()
			if err != nil {
				return
			}
		case <-ping.C:
			c.mu.Lock()
			c.conn.SetWriteDeadline(time.Now().Add(wsWriteWait))
			err := c.conn.WriteMessage(websocket.PingMessage, nil)
			c.mu.Unlock()
			if err != nil {
				return
			}
		}
	}
}

// matchChannels — true, если событие касается хотя бы одного подписанного канала.
// Пустая подписка = ничего не шлём (контракт: subscribe обязателен).
func (c *wsClient) matchChannels(eventChannels []string) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	if len(c.channels) == 0 {
		return false
	}
	for _, ch := range eventChannels {
		if _, ok := c.channels[ch]; ok {
			return true
		}
	}
	return false
}
