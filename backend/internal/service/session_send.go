package service

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"daemon/internal/models"
	"daemon/internal/runtime"
)

// ---------- P2.11: Send to session + verify ----------
//
// Оператор вводит текст в терминал живой сессии (tmux/pi) и получает честный
// статус доставки: delivered (подтверждено capture-м) / staged (введено, не
// подтверждено) / retained (guard: интерактивный prompt — не отправлено).
// process → нет экрана → 409. Верификация — через P2.8 Capture.
//
// Безопасность: RBAC operator+ (sessions.update); текст — literal send-keys
// (без интерпретации); interactive-prompt guard — эвристика (false-pos/neg допустимы).

// SendStatus — статус доставки.
type SendStatus string

const (
	SendStatusDelivered SendStatus = "delivered"
	SendStatusStaged    SendStatus = "staged"
	SendStatusRetained  SendStatus = "retained"
)

// SendToSessionResult — ответ POST /sessions/{id}/send.
type SendToSessionResult struct {
	SessionID int64      `json:"session_id"`
	Status    SendStatus `json:"status"`
	Sent      bool       `json:"sent"`
	Submitted bool       `json:"submitted"`
	Guard     string     `json:"guard,omitempty"`
	Verified  bool       `json:"verified"`
	Tail      []string   `json:"tail,omitempty"`
	SentAt    string     `json:"sent_at"`
}

const (
	sendMaxTextLen     = 2000
	sendGuardLines     = 5  // guard: последние строки для эвристики prompt
	sendVerifyLines    = 30 // verify: последние строки для поиска текста
	sendVerifyAttempts = 3  // ретries верификации
	sendVerifyDelay    = 300 * time.Millisecond
	sendVerifyPartLen  = 25 // «заметная часть»: последние ~N непустых символов
)

// sendVerifySleep — точка инъекции (тесты ставят no-op, чтобы не ждать ретries).
var sendVerifySleep = time.Sleep

// guard-эвристика (конфигурируемые списки). False-positive/false-negative
// допустимы (задокументировано в api-decisions §P2.11).
var (
	interactivePromptPhrases = []string{
		"do you want to",
		"are you sure",
		"password:",
		"permission",
		"type 'y'",
		"y/n",
		"(y/n)",
	}
	interactivePromptSuffixes = []string{">", "#", "$"}
)

// Send — ввести текст в сессию (tmux/pi) + верификация capture-м.
// 404 — сессия нет; 400 — text пуст/длинный; 409 — process (нет экрана) / tmux недоступен/мёртв.
// 200: status delivered/staged/retained (retained — guard: интерактивный prompt).
func (s *SessionService) Send(ctx context.Context, id int64, text string, submit bool) (*SendToSessionResult, error) {
	trimmed := strings.TrimSpace(text)
	if trimmed == "" {
		return nil, NewValidation("text is required")
	}
	if len(text) > sendMaxTextLen {
		return nil, NewValidation(fmt.Sprintf("text too long (max %d chars)", sendMaxTextLen))
	}
	sess, err := s.getSession(ctx, id)
	if err != nil {
		return nil, err // 404
	}
	res := &SendToSessionResult{
		SessionID: sess.ID,
		SentAt:    time.Now().UTC().Format(time.RFC3339Nano),
	}

	switch sess.RuntimeType {
	case "process":
		// у process нет терминала → 409 (retained)
		return nil, NewConflict("process runtime has no terminal to send into")
	default: // tmux / pi
		adapter, err := s.runt.Get(sess.RuntimeType)
		if err != nil {
			return nil, err
		}
		// Guard: интерактивный prompt? (последние ~N строк)
		guardLines, err := adapter.Capture(ctx, sess.RuntimeRef, sendGuardLines)
		if err != nil {
			return nil, sendConflict(err, "screen")
		}
		if reason := interactivePromptGuard(guardLines); reason != "" {
			res.Status = SendStatusRetained
			res.Sent = false
			res.Guard = reason
			s.publishSend(sess, res)
			return res, nil
		}
		// Send
		sr, err := adapter.Send(ctx, sess.RuntimeRef, text, submit)
		if err != nil {
			return nil, sendConflict(err, "send")
		}
		res.Sent = sr.Sent
		res.Submitted = sr.Submitted
		// Verify (reties + capture)
		verified, tail := s.verifySend(ctx, adapter, sess.RuntimeRef, text)
		res.Verified = verified
		if verified {
			res.Status = SendStatusDelivered
			res.Tail = tail
		} else {
			res.Status = SendStatusStaged
		}
		s.publishSend(sess, res)
		return res, nil
	}
}

// sendConflict — маппинг runtime-ошибки на 409 (clear reason), иначе Conflict.
func sendConflict(err error, kind string) *AppError {
	switch {
	case errors.Is(err, runtime.ErrTmuxUnavailable),
		errors.Is(err, runtime.ErrTmuxSessionDead),
		errors.Is(err, runtime.ErrNoScreen):
		return NewConflict(kind + " not available: " + err.Error())
	default:
		return NewConflict(kind + " failed: " + err.Error())
	}
}

// verifySend — после Send: ретries capture + поиск «заметной части» текста.
// (verified, tail при verified). screen пропал → не подтверждено.
func (s *SessionService) verifySend(ctx context.Context, adapter runtime.Adapter, ref, text string) (bool, []string) {
	part := verifyPart(text)
	if part == "" {
		return false, nil
	}
	var last []string
	for attempt := 0; attempt < sendVerifyAttempts; attempt++ {
		if attempt > 0 {
			sendVerifySleep(sendVerifyDelay)
		}
		lines, err := adapter.Capture(ctx, ref, sendVerifyLines)
		if err != nil {
			break // экран пропал → не подтверждено
		}
		last = lines
		if linesContain(lines, part) {
			return true, last
		}
	}
	return false, nil
}

// verifyPart — «заметная часть»: последние ~N символов (case-insensitive).
func verifyPart(text string) string {
	t := strings.TrimSpace(text)
	if len(t) > sendVerifyPartLen {
		t = t[len(t)-sendVerifyPartLen:]
	}
	return strings.ToLower(t)
}

// linesContain — part (lower) в последних строках (join по пробелу — текст может
// переноситься на несколько строк экрана).
func linesContain(lines []string, partLower string) bool {
	if partLower == "" {
		return false
	}
	joined := strings.ToLower(strings.Join(lines, " "))
	return strings.Contains(joined, partLower)
}

// interactivePromptGuard — последняя непустая строка похожа на prompt/permission?
// Возвращает причину (guard) или "". Эвристика: false-pos/neg допустимы.
func interactivePromptGuard(lines []string) string {
	last := ""
	for i := len(lines) - 1; i >= 0; i-- {
		if s := strings.TrimSpace(lines[i]); s != "" {
			last = s
			break
		}
	}
	if last == "" {
		return ""
	}
	lower := strings.ToLower(last)
	for _, p := range interactivePromptPhrases {
		if strings.Contains(lower, p) {
			return "interactive-prompt"
		}
	}
	for _, suf := range interactivePromptSuffixes {
		if strings.HasSuffix(last, suf) {
			return "interactive-prompt"
		}
	}
	return ""
}

// publishSend — событие session.send в EventBus (→ Pulse feed, P2.9).
func (s *SessionService) publishSend(sess *models.Session, r *SendToSessionResult) {
	if s.Bus == nil {
		return
	}
	data := map[string]any{
		"session_id": sess.ID,
		"team_id":    sess.TeamID,
		"status":     string(r.Status),
	}
	s.Bus.Publish(newEvent("session.send", data,
		fmt.Sprintf("session:%d", sess.ID),
		fmt.Sprintf("team:%d", sess.TeamID),
	))
}
