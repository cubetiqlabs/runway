package repository

import (
	"errors"
	"strings"

	"github.com/cubetiqlabs/runway/internal/domain"
	"github.com/jackc/pgx/v5/pgconn"
	"gorm.io/gorm"
)

func clampPage(p domain.PageParams) (int, int) {
	limit := p.Limit
	if limit <= 0 {
		limit = 25
	}
	if limit > 100 {
		limit = 100
	}
	offset := p.Offset
	if offset < 0 {
		offset = 0
	}
	return limit, offset
}

func paginate(p domain.PageParams) func(db *gorm.DB) *gorm.DB {
	limit, offset := clampPage(p)
	return func(db *gorm.DB) *gorm.DB {
		return db.Limit(limit).Offset(offset)
	}
}

func applySort(p domain.PageParams, allowed map[string]string, defaultOrder string) func(db *gorm.DB) *gorm.DB {
	col, ok := allowed[strings.ToLower(strings.TrimSpace(p.SortBy))]
	if !ok {
		return func(db *gorm.DB) *gorm.DB {
			return db.Order(defaultOrder)
		}
	}
	dir := "DESC"
	if strings.EqualFold(strings.TrimSpace(p.SortDir), "asc") {
		dir = "ASC"
	}
	return func(db *gorm.DB) *gorm.DB {
		return db.Order(col + " " + dir)
	}
}

func mapGormError(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return domain.ErrNotFound
	}
	if errors.Is(err, gorm.ErrDuplicatedKey) {
		return domain.ErrConflict
	}
	if errors.Is(err, gorm.ErrForeignKeyViolated) || errors.Is(err, gorm.ErrCheckConstraintViolated) {
		return domain.ErrValidation
	}
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		switch pgErr.Code {
		case "23505":
			return domain.ErrConflict
		case "23503", "23514":
			return domain.ErrValidation
		}
	}
	return err
}

func nonNilSlice[T any](s []T) []T {
	if s == nil {
		return []T{}
	}
	return s
}

func sanitizeLike(s string) string {
	trimmed := strings.TrimSpace(s)
	if trimmed == "" {
		return ""
	}
	trimmed = strings.ReplaceAll(trimmed, `\`, `\\`)
	trimmed = strings.ReplaceAll(trimmed, `%`, `\%`)
	trimmed = strings.ReplaceAll(trimmed, `_`, `\_`)
	return "%" + trimmed + "%"
}
