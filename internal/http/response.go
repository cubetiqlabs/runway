package http

import (
	"errors"
	"strconv"

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
	if raw := c.Query("limit"); raw != "" {
		if v, err := strconv.Atoi(raw); err == nil && v > 0 {
			limit = v
		}
	}
	offset := 0
	if raw := c.Query("offset"); raw != "" {
		if v, err := strconv.Atoi(raw); err == nil && v >= 0 {
			offset = v
		}
	}
	return domain.PageParams{Limit: limit, Offset: offset}
}

func parseUUIDParam(c fiber.Ctx, name string) (uuid.UUID, error) {
	raw := c.Params(name)
	id, err := uuid.Parse(raw)
	if err != nil {
		return uuid.Nil, domain.ErrValidation
	}
	return id, nil
}
