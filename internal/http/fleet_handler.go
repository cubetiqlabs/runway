package http

import (
	"github.com/cubetiqlabs/runway/internal/domain"
	"github.com/cubetiqlabs/runway/internal/service"
	"github.com/gofiber/fiber/v3"
	"github.com/google/uuid"
)

type FleetHandler struct {
	runnerSvc    *service.RunnerService
	dashboardSvc *service.DashboardService
}

func NewFleetHandler(runnerSvc *service.RunnerService, dashboardSvc *service.DashboardService) *FleetHandler {
	return &FleetHandler{runnerSvc: runnerSvc, dashboardSvc: dashboardSvc}
}

func (h *FleetHandler) Overview(c fiber.Ctx) error {
	ov, err := h.dashboardSvc.GetOverview(c.Context())
	if err != nil {
		return err
	}
	return c.JSON(ov)
}

func (h *FleetHandler) ListPools(c fiber.Ctx) error {
	page := parsePage(c)
	var enabled *bool
	if raw := c.Query("enabled"); raw == "true" || raw == "false" {
		b := raw == "true"
		enabled = &b
	}
	filter := domain.PoolFilter{
		Provider:   c.Query("provider"),
		OS:         c.Query("os"),
		Enabled:    enabled,
		Search:     c.Query("search"),
		PageParams: page,
	}
	items, total, err := h.runnerSvc.ListPools(c.Context(), filter)
	if err != nil {
		return err
	}
	return sendPage(c, items, total, page)
}

func (h *FleetHandler) CreatePool(c fiber.Ctx) error {
	var pool domain.RunnerPool
	if err := c.Bind().Body(&pool); err != nil {
		return sendError(c, fiber.StatusBadRequest, "bad_request", "invalid runner pool payload")
	}
	if err := h.runnerSvc.CreatePool(c.Context(), &pool, currentUser(c), c.IP()); err != nil {
		return err
	}
	return c.Status(fiber.StatusCreated).JSON(pool)
}

func (h *FleetHandler) UpdatePool(c fiber.Ctx) error {
	id, err := parseUUIDParam(c, "id")
	if err != nil {
		return err
	}
	var pool domain.RunnerPool
	if err := c.Bind().Body(&pool); err != nil {
		return sendError(c, fiber.StatusBadRequest, "bad_request", "invalid runner pool payload")
	}
	pool.ID = id
	if err := h.runnerSvc.UpdatePool(c.Context(), &pool, currentUser(c), c.IP()); err != nil {
		return err
	}
	return c.JSON(pool)
}

func (h *FleetHandler) DeletePool(c fiber.Ctx) error {
	id, err := parseUUIDParam(c, "id")
	if err != nil {
		return err
	}
	if err := h.runnerSvc.DeletePool(c.Context(), id, currentUser(c), c.IP()); err != nil {
		return err
	}
	return c.JSON(fiber.Map{"ok": true})
}

func (h *FleetHandler) ListRunners(c fiber.Ctx) error {
	page := parsePage(c)
	var poolID *uuid.UUID
	if rawPool := c.Query("pool_id"); rawPool != "" {
		parsed, err := uuid.Parse(rawPool)
		if err != nil {
			return domain.ErrValidation
		}
		poolID = &parsed
	}
	filter := domain.RunnerFilter{
		Status:     c.Query("status"),
		Provider:   c.Query("provider"),
		PoolID:     poolID,
		Search:     c.Query("search"),
		PageParams: page,
	}
	items, total, err := h.runnerSvc.ListRunners(c.Context(), filter)
	if err != nil {
		return err
	}
	return sendPage(c, items, total, page)
}

func (h *FleetHandler) GetRunnerLogs(c fiber.Ctx) error {
	id, err := parseUUIDParam(c, "id")
	if err != nil {
		return err
	}
	page := parsePage(c)
	items, total, err := h.runnerSvc.GetRunnerLogs(c.Context(), id, page)
	if err != nil {
		return err
	}
	return sendPage(c, items, total, page)
}

func (h *FleetHandler) DrainRunner(c fiber.Ctx) error {
	id, err := parseUUIDParam(c, "id")
	if err != nil {
		return err
	}
	rn, err := h.runnerSvc.DrainRunner(c.Context(), id, currentUser(c), c.IP())
	if err != nil {
		return err
	}
	return c.JSON(rn)
}

func (h *FleetHandler) TerminateRunner(c fiber.Ctx) error {
	id, err := parseUUIDParam(c, "id")
	if err != nil {
		return err
	}
	if err := h.runnerSvc.TerminateRunner(c.Context(), id, currentUser(c), c.IP()); err != nil {
		return err
	}
	return c.JSON(fiber.Map{"ok": true})
}

func (h *FleetHandler) ListInstallations(c fiber.Ctx) error {
	page := parsePage(c)
	filter := domain.InstallationFilter{
		Search:     c.Query("search"),
		PageParams: page,
	}
	items, total, err := h.runnerSvc.ListInstallations(c.Context(), filter)
	if err != nil {
		return err
	}
	return sendPage(c, items, total, page)
}

func (h *FleetHandler) SyncInstallations(c fiber.Ctx) error {
	count, err := h.runnerSvc.SyncInstallations(c.Context(), currentUser(c), c.IP())
	if err != nil {
		return err
	}
	return c.JSON(fiber.Map{"synced": count})
}
