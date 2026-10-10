package repository_test

import (
	"context"
	"database/sql"
	"fmt"
	"testing"
	"time"

	"daemon/internal/database"
	"daemon/internal/repository"
)

// ---------- P2.9: EventRepo storage (record + bounded + filters + sort) ----------

func newEventTestDB(t *testing.T) (*repository.EventRepo, *sql.DB, func()) {
	t.Helper()
	db, err := database.Open("sqlite::memory:")
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	db.SetMaxOpenConns(1)
	if err := database.Migrate(context.Background(), db, "sqlite"); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	repo := repository.NewEventRepo(db)
	return repo, db, func() { _ = db.Close() }
}

func TestEventRepoRecordAndSort(t *testing.T) {
	repo, db, close := newEventTestDB(t)
	defer close()
	ctx := context.Background()
	// 3 события с возрастающим created_at
	base := time.Now().UTC().Add(-3 * time.Second)
	for i := 0; i < 3; i++ {
		ev := &repository.EventIn{
			Type: "task.state_changed", EntityKind: "task", EntityID: int64(i + 1),
			Summary:      fmt.Sprintf("Task #%d → in_progress", i+1),
			CreatedAtStr: base.Add(time.Duration(i) * time.Second).Format(time.RFC3339Nano),
		}
		if err := repo.Record(ctx, db, ev); err != nil {
			t.Fatalf("record %d: %v", i, err)
		}
	}
	events, total, err := repo.List(ctx, db, repository.EventListFilter{Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	if total != 3 || len(events) != 3 {
		t.Fatalf("total=%d len=%d, want 3", total, len(events))
	}
	// created_at DESC: последнее (i=2) первым
	if events[0].EntityID != 3 || events[2].EntityID != 1 {
		t.Fatalf("sort: got %d,%d,%d, want 3,2,1", events[0].EntityID, events[1].EntityID, events[2].EntityID)
	}
}

func TestEventRepoFilters(t *testing.T) {
	repo, db, close := newEventTestDB(t)
	defer close()
	ctx := context.Background()
	now := time.Now().UTC()
	tid := int64(7)
	rec := func(typ, kind string, eid, team int64) {
		ev := &repository.EventIn{Type: typ, EntityKind: kind, EntityID: eid, Summary: typ,
			CreatedAtStr: now.Format(time.RFC3339Nano)}
		if team != 0 {
			ev.TeamID = team
		}
		if err := repo.Record(ctx, db, ev); err != nil {
			t.Fatal(err)
		}
	}
	rec("task.state_changed", "task", 1, tid)
	rec("task.created", "task", 2, tid)
	rec("session.started", "session", 1, tid)
	rec("message.sent", "message", 1, 99) // другой team

	// фильтр type
	_, nType, _ := repo.List(ctx, db, repository.EventListFilter{Type: str("task.state_changed")})
	if nType != 1 {
		t.Errorf("type filter: got %d, want 1", nType)
	}
	// фильтр team_id
	_, nTeam, _ := repo.List(ctx, db, repository.EventListFilter{TeamID: i64(tid)})
	if nTeam != 3 {
		t.Errorf("team filter: got %d, want 3", nTeam)
	}
	// фильтр entity_kind
	_, nKind, _ := repo.List(ctx, db, repository.EventListFilter{EntityKind: str("task")})
	if nKind != 2 {
		t.Errorf("entity_kind filter: got %d, want 2", nKind)
	}
	// limit/offset
	evs, _, _ := repo.List(ctx, db, repository.EventListFilter{Limit: 2, Offset: 0})
	if len(evs) != 2 {
		t.Errorf("limit=2: got %d, want 2", len(evs))
	}
}

func TestEventRepoBoundedCleanup(t *testing.T) {
	repo, db, close := newEventTestDB(t)
	defer close()
	ctx := context.Background()
	// вставляем 5001 напрямую (без prune на каждую запись — быстро)
	base := time.Now().UTC().Add(-time.Hour)
	for i := 0; i < 5001; i++ {
		_, err := db.Exec(
			"INSERT INTO events (type, entity_kind, entity_id, summary, created_at) VALUES ('t','task',?,'s',?)",
			i+1, base.Add(time.Duration(i)*time.Millisecond).Format(time.RFC3339Nano))
		if err != nil {
			t.Fatalf("insert %d: %v", i, err)
		}
	}
	if err := repo.Prune(ctx, db); err != nil {
		t.Fatal(err)
	}
	var count int
	if err := db.QueryRow("SELECT COUNT(*) FROM events").Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count > 5000 {
		t.Errorf("bounded: kept %d, want <= 5000", count)
	}
	if count != 5000 {
		t.Errorf("bounded: kept %d, want 5000", count)
	}
}

func str(s string) *string { return &s }
func i64(v int64) *int64   { return &v }
