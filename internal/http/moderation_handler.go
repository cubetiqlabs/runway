package http

import (
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
	items, total, err := h.modSvc.ListPolicies(c.Context(), parsePage(c))
	if err != nil {
		return err
	}
	return c.JSON(fiber.Map{"items": items, "total": total})
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
	filter := domain.JobFilter{
		Status:     c.Query("status"),
		Repo:       c.Query("repo"),
		Search:     c.Query("search"),
		PageParams: parsePage(c),
	}
	items, total, err := h.modSvc.ListJobs(c.Context(), filter)
	if err != nil {
		return err
	}
	return c.JSON(fiber.Map{"items": items, "total": total})
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
	items, total, err := h.dashboardSvc.ListDeliveries(c.Context(), parsePage(c))
	if err != nil {
		return err
	}
	return c.JSON(fiber.Map{"items": items, "total": total})
}

func (h *ModerationHandler) ListAuditLogs(c fiber.Ctx) error {
	items, total, err := h.dashboardSvc.ListAuditLogs(c.Context(), parsePage(c))
	if err != nil {
		return err
	}
	return c.JSON(fiber.Map{"items": items, "total": total})
}

func (h *ModerationHandler) SettingsStatus(c fiber.Ctx) error {
	return c.JSON(h.dashboardSvc.GetSettingsStatus())
}
