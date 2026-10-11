package service

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"time"

	"daemon/internal/models"
	"daemon/internal/repository"
)

// EvidenceService — P3.13: proof/evidence на задачах (submit/list/verdict).
// «Closure ≠ acceptance»: evidence-вердикт (accept/reject/withdraw) — это
// acceptance, отдельное от task.state (done/canceled). Терминальная задача
// может принимать verdict (acceptance ≠ closure).
type EvidenceService struct {
	db       *sql.DB
	evidence *repository.EvidenceRepo
	tasks    *repository.TaskRepo
}

func NewEvidenceService(db *sql.DB, stores *repository.Stores) *EvidenceService {
	return &EvidenceService{db: db, evidence: stores.Evidence, tasks: stores.Tasks}
}

// EvidenceView — артефакт-доказательство в формате контракта.
type EvidenceView struct {
	ID            int64   `json:"id"`
	TaskID        int64   `json:"task_id"`
	Type          string  `json:"type"`
	Name          string  `json:"name"`
	ContentRef    *string `json:"content_ref,omitempty"`
	ContentSHA    *string `json:"content_sha,omitempty"`
	Description   *string `json:"description,omitempty"`
	Status        string  `json:"status"`
	VerdictReason *string `json:"verdict_reason,omitempty"`
	SubmittedBy   string  `json:"submitted_by"`
	JudgedBy      *string `json:"judged_by,omitempty"`
	JudgedAt      *string `json:"judged_at,omitempty"`
	CreatedAt     string  `json:"created_at"`
	UpdatedAt     string  `json:"updated_at"`
}

func evidenceView(e *models.TaskEvidence) *EvidenceView {
	v := &EvidenceView{
		ID: e.ID, TaskID: e.TaskID, Type: string(e.Type), Name: e.Name,
		ContentRef: e.ContentRef, ContentSHA: e.ContentSHA, Description: e.Description,
		Status: string(e.Status), VerdictReason: e.VerdictReason,
		SubmittedBy: e.SubmittedBy, JudgedBy: e.JudgedBy,
		CreatedAt: e.CreatedAt.Format(rfc3339), UpdatedAt: e.UpdatedAt.Format(rfc3339),
	}
	if e.JudgedAt != nil {
		v.JudgedAt = ptr(e.JudgedAt.Format(rfc3339))
	}
	return v
}

// EvidenceViewFor — exported-обёртка (для HTTP-хендлеров).
func EvidenceViewFor(e *models.TaskEvidence) *EvidenceView { return evidenceView(e) }

// ---------- Submit ----------

type SubmitEvidenceRequest struct {
	TaskID      int64
	Type        models.EvidenceType
	Name        string
	Content     *string // inline контент (серверный sha256 = candidate-sha)
	ContentRef  *string // внешняя ссылка/путь/метка
	ContentSHA  *string // sha (для content_ref; inline игнорируется)
	Description *string
	SubmittedBy string // атрибуция (из auth-контекста; "human" по умолчанию)
}

// Submit — создать артефакт (status=submitted). Валидация: etype valid, name
// обязателен, либо content либо content_ref. candidate-sha: inline → серверный
// sha256; ref → sha из body (если задан).
func (s *EvidenceService) Submit(ctx context.Context, req SubmitEvidenceRequest) (*models.TaskEvidence, error) {
	if !req.Type.Valid() {
		return nil, NewValidation("invalid evidence type", []FieldError{{Field: "type", Reason: "must be screenshot|test|log|other"}})
	}
	if req.Name == "" {
		return nil, NewValidation("name is required", []FieldError{{Field: "name", Reason: "required"}})
	}
	hasContent := req.Content != nil && *req.Content != ""
	hasRef := req.ContentRef != nil && *req.ContentRef != ""
	if !hasContent && !hasRef {
		return nil, NewValidation("content or content_ref is required", []FieldError{{Field: "content", Reason: "content or content_ref required"}})
	}
	if _, err := s.requireTask(ctx, req.TaskID); err != nil {
		return nil, err
	}
	// candidate-sha: inline → серверный sha256 (целостность); ref → из body.
	var sha *string
	switch {
	case hasContent:
		sum := sha256.Sum256([]byte(*req.Content))
		h := hex.EncodeToString(sum[:])
		sha = &h
	case hasRef && req.ContentSHA != nil && *req.ContentSHA != "":
		sha = req.ContentSHA
	}
	e := &models.TaskEvidence{
		TaskID: req.TaskID, Type: req.Type, Name: req.Name,
		ContentRef: req.ContentRef, ContentSHA: sha, Description: req.Description,
		Status:      models.EvidenceSubmitted,
		SubmittedBy: orDefault(req.SubmittedBy, "human"),
	}
	if err := s.evidence.Create(ctx, s.db, e); err != nil {
		return nil, err
	}
	return e, nil
}

// ---------- List ----------

// List — bounded-список артефактов задачи (новые сверху). 404 (нет задачи).
func (s *EvidenceService) List(ctx context.Context, taskID int64, limit, offset int) ([]*EvidenceView, int, error) {
	if _, err := s.requireTask(ctx, taskID); err != nil {
		return nil, 0, err
	}
	items, total, err := s.evidence.ListByTask(ctx, s.db, taskID, limit, offset)
	if err != nil {
		return nil, 0, err
	}
	out := make([]*EvidenceView, 0, len(items))
	for _, it := range items {
		out = append(out, evidenceView(it))
	}
	return out, total, nil
}

// ---------- Verdict (judge) ----------

type VerdictEvidenceRequest struct {
	TaskID     int64
	EvidenceID int64
	Action     models.EvidenceAction
	Reason     *string
	JudgedBy   string // атрибуция (из auth-контекста; "human" по умолчанию)
}

// Verdict — применить вердикт судьи (accept/reject/withdraw). Правила:
//   - accept/reject — устанавливает/обновляет status (последний вердикт побеждает).
//   - withdraw — только если уже есть вердикт (accept/reject → withdrawn);
//     из submitted → 400.
//
// Терминальная задача (done/canceled) — вердикт допустим (acceptance ≠ closure).
func (s *EvidenceService) Verdict(ctx context.Context, req VerdictEvidenceRequest) (*models.TaskEvidence, error) {
	if !req.Action.Valid() {
		return nil, NewValidation("invalid action", []FieldError{{Field: "action", Reason: "must be accept|reject|withdraw"}})
	}
	if _, err := s.requireTask(ctx, req.TaskID); err != nil {
		return nil, err
	}
	// артефакт существует + принадлежит задаче (cross-task → 404).
	cur, err := s.evidence.GetByTask(ctx, s.db, req.TaskID, req.EvidenceID)
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return nil, notFound("evidence")
		}
		return nil, err
	}
	if req.Action == models.EvidenceWithdraw && cur.Status == models.EvidenceSubmitted {
		return nil, NewValidation("cannot withdraw an unjudged evidence",
			[]FieldError{{Field: "action", Reason: "withdraw requires a prior verdict (accept/reject)"}})
	}
	now := time.Now().UTC()
	updated, err := s.evidence.SetVerdict(ctx, s.db, req.EvidenceID, req.Action.VerdictStatus(), req.Reason, orDefault(req.JudgedBy, "human"), now)
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return nil, notFound("evidence")
		}
		return nil, err
	}
	return updated, nil
}

func (s *EvidenceService) requireTask(ctx context.Context, taskID int64) (*models.Task, error) {
	t, err := s.tasks.GetByID(ctx, s.db, taskID)
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return nil, notFound("task")
		}
		return nil, err
	}
	return t, nil
}

func orDefault(s, def string) string {
	if s == "" {
		return def
	}
	return s
}

// ---------- Readiness / Acceptance (агрегаты по статусам) ----------
// Используются в TaskService.viewsFor (batched через StatusSummaryByTasks).

// EvidenceReadiness — "ready" если ≥1 accepted; "pending" если есть evidence (но
// ни одного accepted); "none" если evidence нет. (решение, задокументировано)
func EvidenceReadiness(counts map[string]int) string {
	if counts[string(models.EvidenceAccepted)] > 0 {
		return "ready"
	}
	if EvidenceTotal(counts) > 0 {
		return "pending"
	}
	return "none"
}

// EvidenceAcceptance — агрегат (отдельно от task.state): есть accepted →
// "accepted"; иначе есть rejected → "rejected"; иначе есть submitted → "pending";
// иначе "none". (closure ≠ acceptance)
func EvidenceAcceptance(counts map[string]int) string {
	switch {
	case counts[string(models.EvidenceAccepted)] > 0:
		return "accepted"
	case counts[string(models.EvidenceRejected)] > 0:
		return "rejected"
	case counts[string(models.EvidenceSubmitted)] > 0:
		return "pending"
	default:
		return "none"
	}
}

// EvidenceTotal — суммарное число evidence по статусам.
func EvidenceTotal(counts map[string]int) int {
	total := 0
	for _, c := range counts {
		total += c
	}
	return total
}
