package models

import "time"

// ProjectKind — тип проекта.
type ProjectKind string

const (
	ProjectExisting ProjectKind = "existing" // каталог уже существует
	ProjectNew      ProjectKind = "new"      // daemon создаёт каталог (+ git init)
)

func (k ProjectKind) Valid() bool {
	return k == ProjectExisting || k == ProjectNew
}

// Project — рабочая директория (OpenRIG work tree). «Где работать» — свойство
// работы, а не только параметр запуска.
type Project struct {
	ID          int64
	Name        string
	Path        string // абсолютный путь; immutable после создания
	Kind        ProjectKind
	Git         bool
	Description string
	Archived    bool
	CreatedAt   time.Time
	UpdatedAt   time.Time
}
