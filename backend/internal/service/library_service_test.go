package service_test

import (
	"context"
	"database/sql"
	"testing"

	"daemon/internal/database"
	"daemon/internal/models"
	"daemon/internal/repository"
	"daemon/internal/service"
)

func setupLibraryEnv(t *testing.T) (*service.LibraryService, *repository.Stores, *sql.DB, context.Context) {
	t.Helper()
	db, err := database.Open("sqlite::memory:")
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	db.SetMaxOpenConns(1)
	t.Cleanup(func() { db.Close() })
	if err := database.Migrate(context.Background(), db, "sqlite"); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	stores := repository.NewStores(db)
	teamSvc := service.NewTeamService(db, stores)
	teamSvc.SpecsDir = t.TempDir()
	lsvc := service.NewLibraryService(db, stores, teamSvc)
	return lsvc, stores, db, context.Background()
}

// newLibraryItem — создаёт элемент библиотеки напрямую через repo (rename-тест не
// зависит от снапшота источника).
func newLibraryItem(t *testing.T, stores *repository.Stores, db *sql.DB, ctx context.Context, name string) *models.LibraryItem {
	t.Helper()
	it := &models.LibraryItem{Type: models.LibTeam, Name: name, Group: "general", Version: "1.0.0", Spec: map[string]any{}}
	if err := stores.Library.Create(ctx, db, it); err != nil {
		t.Fatalf("create library item: %v", err)
	}
	return it
}

func TestUpdateLibraryItemRename(t *testing.T) {
	lsvc, stores, db, ctx := setupLibraryEnv(t)
	it := newLibraryItem(t, stores, db, ctx, "orig")

	// rename без description → сохраняем текущий (nil)
	view, err := lsvc.Update(ctx, service.UpdateLibraryRequest{ID: it.ID, Name: "renamed"})
	if err != nil {
		t.Fatalf("rename: %v", err)
	}
	if view.Name != "renamed" {
		t.Fatalf("want renamed, got %q", view.Name)
	}
	got, _, _, err := lsvc.Get(ctx, it.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Name != "renamed" {
		t.Fatalf("persisted name: %q", got.Name)
	}

	// rename + description
	desc := "d2"
	view, err = lsvc.Update(ctx, service.UpdateLibraryRequest{ID: it.ID, Name: "renamed2", Description: &desc})
	if err != nil {
		t.Fatal(err)
	}
	if view.Name != "renamed2" || view.Description == nil || *view.Description != "d2" {
		t.Fatalf("after 2nd rename: %q / %v", view.Name, view.Description)
	}

	// not_found
	if _, err := lsvc.Update(ctx, service.UpdateLibraryRequest{ID: 999999, Name: "x"}); err == nil {
		t.Fatal("want not_found")
	} else if ae, ok := err.(*service.AppError); !ok || ae.Code != "not_found" {
		t.Fatalf("want not_found, got %v", err)
	}

	// validation: пустое имя
	if _, err := lsvc.Update(ctx, service.UpdateLibraryRequest{ID: it.ID, Name: " "}); err == nil {
		t.Fatal("want validation_failed on empty name")
	} else if ae, ok := err.(*service.AppError); !ok || ae.Code != "validation_failed" {
		t.Fatalf("want validation_failed, got %v", err)
	}
}
