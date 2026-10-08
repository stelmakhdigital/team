package service

import (
	"sync"
	"time"
)

// WSEvent — событие real-time шины (контракт 20 §WebSocket).
// Сериализуется как {"type", "data", "timestamp"} — WSServerMessage.
// Channels — внутренние маршрутизационные каналы (team:1, task:5, session:3,
// chatroom:2, watchdog:4); WS-клиент получает событие, если подписан хотя бы на один.
type WSEvent struct {
	Type      string   `json:"type"`
	Data      any      `json:"data"`
	Timestamp string   `json:"timestamp"`
	Channels  []string `json:"-"`
}

func newEvent(typ string, data any, channels ...string) WSEvent {
	// «dashboard» — глобальный канал UI (frontend DashboardPage подписан на него):
	// каждое событие дублируется туда.
	for _, ch := range channels {
		if ch == "dashboard" {
			return WSEvent{Type: typ, Data: data, Timestamp: time.Now().UTC().Format(time.RFC3339), Channels: channels}
		}
	}
	channels = append(channels, "dashboard")
	return WSEvent{Type: typ, Data: data, Timestamp: time.Now().UTC().Format(time.RFC3339), Channels: channels}
}

// EventBus — in-memory pub/sub для WS-событий.
// Slice 5 подключит WS-хендлер /ws (Subscribe); публикация — из сервисов.
// Publish неблокирующий: медленный подписчик может потерять события
// (буфер 64) — для UI-уведомлений допустимо, источники истины — REST.
type EventBus struct {
	mu   sync.RWMutex
	subs map[chan WSEvent]struct{}
}

func NewEventBus() *EventBus {
	return &EventBus{subs: make(map[chan WSEvent]struct{})}
}

// Publish рассылает событие всем подписчикам (не блокируется).
func (b *EventBus) Publish(e WSEvent) {
	if b == nil {
		return
	}
	b.mu.RLock()
	defer b.mu.RUnlock()
	for ch := range b.subs {
		select {
		case ch <- e:
		default: // подписчик не успевает — event dropped
		}
	}
}

// Subscribe возвращает канал событий и функцию отписки.
func (b *EventBus) Subscribe() (<-chan WSEvent, func()) {
	ch := make(chan WSEvent, 64)
	b.mu.Lock()
	b.subs[ch] = struct{}{}
	b.mu.Unlock()
	unsub := func() {
		b.mu.Lock()
		delete(b.subs, ch)
		b.mu.Unlock()
	}
	return ch, unsub
}
