package service

import (
	"context"
	"database/sql"

	"daemon/internal/models"
	"daemon/internal/repository"
)

// AuditService — запись/чтение audit log (контракт 20 §6.3).
type AuditService struct {
	db   *sql.DB
	repo *repository.AuditRepo
}

func NewAuditService(db *sql.DB, s *repository.Stores) *AuditService {
	return &AuditService{db: db, repo: s.Audit}
}

// Record — append audit-записи (ошибки логгируются, не прерывают запрос).
func (s *AuditService) Record(ctx context.Context, e models.AuditEntry) error {
	if s == nil || s.repo == nil {
		return nil
	}
	return s.repo.Create(ctx, s.db, &e)
}

// List — GET /api/v1/audit.
func (s *AuditService) List(ctx context.Context, f repository.AuditFilter) ([]*models.AuditEntry, int, error) {
	return s.repo.List(ctx, s.db, f)
}
