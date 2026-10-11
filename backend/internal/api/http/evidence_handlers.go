package httpapi

import (
	"net/http"

	"daemon/internal/models"
	"daemon/internal/service"
)

// ---------- P3.13: Proof/evidence на задачах ----------

// actorName — атрибуция (submitted_by / judged_by): из auth-контекста
// (user / apikey / role), иначе "human" (auth выключен).
func actorName(r *http.Request) string {
	if c := authContext(r.Context()); c != nil && c.UserName != "" {
		return c.UserName
	}
	return "human"
}

type submitEvidenceBody struct {
	Type        string  `json:"type"`
	Name        string  `json:"name"`
	Content     *string `json:"content"`
	ContentRef  *string `json:"content_ref"`
	ContentSHA  *string `json:"content_sha"`
	Description *string `json:"description"`
}

// SubmitEvidence — POST /api/v1/tasks/{id}/evidence (RBAC tasks.update, operator+).
// Создаёт артефакт (status=submitted). candidate-sha: inline → серверный sha256;
// ref → sha из body. submitted_by — атрибуция (auth-контекст).
func (h *handlers) SubmitEvidence(w http.ResponseWriter, r *http.Request) {
	id, err := parseIDParam(r.PathValue("id"))
	if err != nil {
		writeError(w, r, err)
		return
	}
	var body submitEvidenceBody
	if err := decodeJSON(r, &body); err != nil {
		writeError(w, r, err)
		return
	}
	ev, err := h.evsvc.Submit(r.Context(), service.SubmitEvidenceRequest{
		TaskID: id, Type: models.EvidenceType(body.Type), Name: body.Name,
		Content: body.Content, ContentRef: body.ContentRef, ContentSHA: body.ContentSHA,
		Description: body.Description, SubmittedBy: actorName(r),
	})
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, service.EvidenceViewFor(ev))
}

// ListEvidence — GET /api/v1/tasks/{id}/evidence?limit=&offset= (RBAC dashboard.read, viewer-ok).
func (h *handlers) ListEvidence(w http.ResponseWriter, r *http.Request) {
	id, err := parseIDParam(r.PathValue("id"))
	if err != nil {
		writeError(w, r, err)
		return
	}
	limit, offset := limitOffset(r.URL.Query())
	items, total, err := h.evsvc.List(r.Context(), id, limit, offset)
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"evidence": items, "total": total})
}

type verdictEvidenceBody struct {
	Action string  `json:"action"`
	Reason *string `json:"reason"`
}

// VerdictEvidence — POST /api/v1/tasks/{id}/evidence/{evid_id}/verdict
// (RBAC tasks.update, operator+ — судит). accept/reject/withdraw + reason.
// judged_by — атрибуция (auth-контекст). Терминальная задача — вердикт допустим.
func (h *handlers) VerdictEvidence(w http.ResponseWriter, r *http.Request) {
	id, err := parseIDParam(r.PathValue("id"))
	if err != nil {
		writeError(w, r, err)
		return
	}
	evidID, err := parseIDParam(r.PathValue("evid_id"))
	if err != nil {
		writeError(w, r, err)
		return
	}
	var body verdictEvidenceBody
	if err := decodeJSON(r, &body); err != nil {
		writeError(w, r, err)
		return
	}
	ev, err := h.evsvc.Verdict(r.Context(), service.VerdictEvidenceRequest{
		TaskID: id, EvidenceID: evidID, Action: models.EvidenceAction(body.Action),
		Reason: body.Reason, JudgedBy: actorName(r),
	})
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, service.EvidenceViewFor(ev))
}
