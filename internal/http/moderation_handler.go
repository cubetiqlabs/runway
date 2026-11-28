package http

import (
	"strings"
	"time"

	"github.com/cubetiqlabs/runway/internal/domain"
	"github.com/cubetiqlabs/runway/internal/moderation"
	"github.com/cubetiqlabs/runway/internal/service"
	"github.com/gofiber/fiber/v3"
)

type ModerationHandler struct {
	modSvc       *service.ModerationService
	dashboardSvc *service.DashboardService
}

func NewModerationHandler(modSvc *service.ModerationService, dashboardSvc *service.DashboardService) *ModerationHandler {
	return &ModerationHandler{modSvc: modSvc, dashboardSvc: dashboardSvc}
}

func (h *ModerationHandler) ListPolicies(c fiber.Ctx) error {
	page := parsePage(c)
	var enabled *bool
	if raw := c.Query("enabled"); raw == "true" || raw == "false" {
		b := raw == "true"
		enabled = &b
	}
	filter := domain.PolicyFilter{
		Action:     c.Query("action"),
		Enabled:    enabled,
		Search:     c.Query("search"),
		PageParams: page,
	}
	items, total, err := h.modSvc.ListPolicies(c.Context(), filter)
	if err != nil {
		return err
	}
	return sendPage(c, items, total, page)
}

func (h *ModerationHandler) CreatePolicy(c fiber.Ctx) error {
	var policy domain.ModerationPolicy
	if err := c.Bind().Body(&policy); err != nil {
		return sendError(c, fiber.StatusBadRequest, "bad_request", "invalid policy payload")
	}
	if err := h.modSvc.CreatePolicy(c.Context(), &policy, currentUser(c), c.IP()); err != nil {
		return err
	}
	return c.Status(fiber.StatusCreated).JSON(policy)
}

func (h *ModerationHandler) UpdatePolicy(c fiber.Ctx) error {
	id, err := parseUUIDParam(c, "id")
	if err != nil {
		return err
	}
	var policy domain.ModerationPolicy
	if err := c.Bind().Body(&policy); err != nil {
		return sendError(c, fiber.StatusBadRequest, "bad_request", "invalid policy payload")
	}
	policy.ID = id
	if err := h.modSvc.UpdatePolicy(c.Context(), &policy, currentUser(c), c.IP()); err != nil {
		return err
	}
	return c.JSON(policy)
}

func (h *ModerationHandler) DeletePolicy(c fiber.Ctx) error {
	id, err := parseUUIDParam(c, "id")
	if err != nil {
		return err
	}
	if err := h.modSvc.DeletePolicy(c.Context(), id, currentUser(c), c.IP()); err != nil {
		return err
	}
	return c.JSON(fiber.Map{"ok": true})
}

func (h *ModerationHandler) SimulatePolicy(c fiber.Ctx) error {
	var jobCtx moderation.JobContext
	if err := c.Bind().Body(&jobCtx); err != nil {
		return sendError(c, fiber.StatusBadRequest, "bad_request", "invalid simulation payload")
	}
	dec, pool, err := h.modSvc.Simulate(c.Context(), jobCtx)
	if err != nil {
		return err
	}
	return c.JSON(fiber.Map{
		"decision":     dec,
		"matched_pool": pool,
	})
}

func (h *ModerationHandler) ListJobs(c fiber.Ctx) error {
	page := parsePage(c)
	filter := domain.JobFilter{
		Status:     c.Query("status"),
		Repo:       c.Query("repo"),
		Search:     c.Query("search"),
		PageParams: page,
	}
	items, total, err := h.modSvc.ListJobs(c.Context(), filter)
	if err != nil {
		return err
	}
	return sendPage(c, items, total, page)
}

func (h *ModerationHandler) ApproveJob(c fiber.Ctx) error {
	id, err := parseUUIDParam(c, "id")
	if err != nil {
		return err
	}
	var req struct {
		Reason string `json:"reason"`
	}
	_ = c.Bind().Body(&req)
	job, err := h.modSvc.ApproveJob(c.Context(), id, req.Reason, currentUser(c), c.IP())
	if err != nil {
		return err
	}
	return c.JSON(job)
}

func (h *ModerationHandler) RejectJob(c fiber.Ctx) error {
	id, err := parseUUIDParam(c, "id")
	if err != nil {
		return err
	}
	var req struct {
		Reason string `json:"reason"`
	}
	_ = c.Bind().Body(&req)
	job, err := h.modSvc.RejectJob(c.Context(), id, req.Reason, currentUser(c), c.IP())
	if err != nil {
		return err
	}
	return c.JSON(job)
}

func (h *ModerationHandler) ListDeliveries(c fiber.Ctx) error {
	page := parsePage(c)
	filter := domain.DeliveryFilter{
		Status:     c.Query("status"),
		EventName:  c.Query("event_name"),
		Search:     c.Query("search"),
		PageParams: page,
	}
	items, total, err := h.dashboardSvc.ListDeliveries(c.Context(), filter)
	if err != nil {
		return err
	}
	return sendPage(c, items, total, page)
}

func (h *ModerationHandler) ListAuditLogs(c fiber.Ctx) error {
	page := parsePage(c)
	filter := domain.AuditFilter{
		Action:       c.Query("action"),
		ResourceType: c.Query("resource_type"),
		Search:       c.Query("search"),
		PageParams:   page,
	}
	items, total, err := h.dashboardSvc.ListAuditLogs(c.Context(), filter)
	if err != nil {
		return err
	}
	return sendPage(c, items, total, page)
}

func (h *ModerationHandler) ListEvents(c fiber.Ctx) error {
	page := parsePage(c)
	var since *time.Time
	if rawWindow := strings.TrimSpace(c.Query("window")); rawWindow != "" {
		if !strings.EqualFold(rawWindow, "all") {
			t := time.Now().UTC().Add(-parseWindowDuration(rawWindow, 24*time.Hour))
			since = &t
		}
	} else if rawTS := strings.TrimSpace(c.Query("since")); rawTS != "" {
		if parsed, err := time.Parse(time.RFC3339, rawTS); err == nil {
			u := parsed.UTC()
			since = &u
		}
	}
	var until *time.Time
	if rawUntil := c.Query("until"); rawUntil != "" {
		if parsed, err := time.Parse(time.RFC3339, rawUntil); err == nil {
			u := parsed.UTC()
			until = &u
		}
	}
	filter := domain.EventFilter{
		Category:      c.Query("category"),
		EventType:     c.Query("event_type"),
		Severity:      c.Query("severity"),
		ResourceType:  c.Query("resource_type"),
		ResourceID:    c.Query("resource_id"),
		CorrelationID: c.Query("correlation_id"),
		Repo:          c.Query("repo"),
		Search:        c.Query("search"),
		Since:         since,
		Until:         until,
		PageParams:    page,
	}
	items, total, err := h.dashboardSvc.ListEvents(c.Context(), filter)
	if err != nil {
		return err
	}
	return sendPage(c, items, total, page)
}

func (h *ModerationHandler) EventSummary(c fiber.Ctx) error {
	rawWindow := strings.TrimSpace(c.Query("window"))
	var since time.Time
	if !strings.EqualFold(rawWindow, "all") {
		since = time.Now().UTC().Add(-parseWindowDuration(rawWindow, 24*time.Hour))
	}
	sum, err := h.dashboardSvc.GetEventSummary(c.Context(), since)
	if err != nil {
		return err
	}
	return c.JSON(sum)
}

func (h *ModerationHandler) SettingsStatus(c fiber.Ctx) error {
	return c.JSON(h.dashboardSvc.GetSettingsStatus(c.Context()))
}
