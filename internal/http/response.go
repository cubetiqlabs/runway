package http

import (
	"errors"
	"strconv"
	"strings"
	"time"

	"github.com/cubetiqlabs/runway/internal/domain"
	"github.com/gofiber/fiber/v3"
	"github.com/gofiber/fiber/v3/middleware/requestid"
	"github.com/google/uuid"
)

type APIError struct {
	Code      string `json:"code"`
	Message   string `json:"message"`
	RequestID string `json:"request_id,omitempty"`
}

func sendError(c fiber.Ctx, status int, code, msg string) error {
	return c.Status(status).JSON(fiber.Map{
		"error": APIError{
			Code:      code,
			Message:   msg,
			RequestID: requestid.FromContext(c),
		},
	})
}

func mapDomainError(err error) (int, string, string) {
	switch {
	case errors.Is(err, domain.ErrNotFound):
		return fiber.StatusNotFound, "not_found", err.Error()
	case errors.Is(err, domain.ErrConflict):
		return fiber.StatusConflict, "conflict", err.Error()
	case errors.Is(err, domain.ErrUnauthorized):
		return fiber.StatusUnauthorized, "unauthorized", "authentication required or invalid credentials"
	case errors.Is(err, domain.ErrForbidden):
		return fiber.StatusForbidden, "forbidden", "insufficient permissions"
	case errors.Is(err, domain.ErrValidation):
		return fiber.StatusBadRequest, "validation_error", err.Error()
	case errors.Is(err, domain.ErrPolicyDenied):
		return fiber.StatusForbidden, "policy_denied", err.Error()
	default:
		return fiber.StatusInternalServerError, "internal_error", "an unexpected error occurred"
	}
}

func parsePage(c fiber.Ctx) domain.PageParams {
	limit := 25
	rawLimit := c.Query("limit")
	if rawLimit == "" {
		rawLimit = c.Query("page_size")
	}
	if rawLimit != "" {
		if v, err := strconv.Atoi(rawLimit); err == nil && v > 0 {
			if v > 200 {
				v = 200
			}
			limit = v
		}
	}
	offset := 0
	if raw := c.Query("offset"); raw != "" {
		if v, err := strconv.Atoi(raw); err == nil && v >= 0 {
			offset = v
		}
	} else if rawPage := c.Query("page"); rawPage != "" {
		if p, err := strconv.Atoi(rawPage); err == nil && p > 1 {
			offset = (p - 1) * limit
		}
	}
	sortDir := strings.ToLower(strings.TrimSpace(c.Query("sort_dir")))
	if sortDir != "asc" && sortDir != "desc" {
		sortDir = ""
	}
	return domain.PageParams{
		Limit:   limit,
		Offset:  offset,
		SortBy:  strings.TrimSpace(c.Query("sort_by")),
		SortDir: sortDir,
	}
}

func sendPage[T any](c fiber.Ctx, items []T, total int, page domain.PageParams) error {
	if items == nil {
		items = []T{}
	}
	limit := page.Limit
	if limit <= 0 {
		limit = 25
	}
	offset := page.Offset
	if offset < 0 {
		offset = 0
	}
	currentPage := (offset / limit) + 1
	totalPages := 1
	if total > 0 {
		totalPages = (total + limit - 1) / limit
	}
	hasMore := offset+len(items) < total
	return c.JSON(fiber.Map{
		"items":       items,
		"total":       total,
		"limit":       limit,
		"offset":      offset,
		"page":        currentPage,
		"total_pages": totalPages,
		"has_more":    hasMore,
	})
}

func parseWindowDuration(raw string, fallback time.Duration) time.Duration {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return fallback
	}
	var d time.Duration
	if strings.HasSuffix(trimmed, "d") || strings.HasSuffix(trimmed, "D") {
		numPart := strings.TrimSpace(trimmed[:len(trimmed)-1])
		if days, err := strconv.Atoi(numPart); err == nil && days > 0 {
			d = time.Duration(days) * 24 * time.Hour
		}
	} else if parsed, err := time.ParseDuration(trimmed); err == nil {
		d = parsed
	}
	if d <= 0 {
		return fallback
	}
	if d > 90*24*time.Hour {
		return 90 * 24 * time.Hour
	}
	return d
}

func parseUUIDParam(c fiber.Ctx, name string) (uuid.UUID, error) {
	raw := c.Params(name)
	id, err := uuid.Parse(raw)
	if err != nil {
		return uuid.Nil, domain.ErrValidation
	}
	return id, nil
}
