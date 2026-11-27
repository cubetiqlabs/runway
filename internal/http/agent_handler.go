package http

import (
	"strconv"
	"time"

	"github.com/cubetiqlabs/runway/internal/domain"
	"github.com/cubetiqlabs/runway/internal/service"
	"github.com/gofiber/fiber/v3"
	"github.com/google/uuid"
)

type AgentHandler struct {
	agentSvc *service.AgentService
}

func NewAgentHandler(agentSvc *service.AgentService) *AgentHandler {
	return &AgentHandler{agentSvc: agentSvc}
}

func (h *AgentHandler) ListAgents(c fiber.Ctx) error {
	page := parsePage(c)
	if h.agentSvc == nil {
		return sendPage(c, []domain.Agent{}, 0, page)
	}
	var poolID *uuid.UUID
	if rawPool := c.Query("pool_id"); rawPool != "" {
		parsed, err := uuid.Parse(rawPool)
		if err != nil {
			return domain.ErrValidation
		}
		poolID = &parsed
	}
	filter := domain.AgentFilter{
		Status:     c.Query("status"),
		OS:         c.Query("os"),
		Runtime:    c.Query("runtime"),
		PoolID:     poolID,
		Search:     c.Query("search"),
		PageParams: page,
	}
	items, total, err := h.agentSvc.ListAgents(c.Context(), filter)
	if err != nil {
		return err
	}
	return sendPage(c, items, total, page)
}

func (h *AgentHandler) GetAgent(c fiber.Ctx) error {
	if h.agentSvc == nil {
		return domain.ErrNotFound
	}
	id, err := parseUUIDParam(c, "id")
	if err != nil {
		return err
	}
	window := parseWindowDuration(c.Query("window"), time.Hour)
	detail, err := h.agentSvc.GetAgentDetail(c.Context(), id, window)
	if err != nil {
		return err
	}
	return c.JSON(detail)
}

func (h *AgentHandler) GetAgentMetrics(c fiber.Ctx) error {
	if h.agentSvc == nil {
		return c.JSON(fiber.Map{"items": []domain.AgentMetricPoint{}})
	}
	id, err := parseUUIDParam(c, "id")
	if err != nil {
		return err
	}
	window := parseWindowDuration(c.Query("window"), time.Hour)
	limit := 240
	if raw := c.Query("limit"); raw != "" {
		if v, convErr := strconv.Atoi(raw); convErr == nil && v > 0 {
			limit = v
		}
	}
	points, err := h.agentSvc.GetAgentMetrics(c.Context(), id, window, limit)
	if err != nil {
		return err
	}
	return c.JSON(fiber.Map{"items": points})
}

func (h *AgentHandler) ListAgentTasks(c fiber.Ctx) error {
	page := parsePage(c)
	if h.agentSvc == nil {
		return sendPage(c, []domain.AgentTask{}, 0, page)
	}
	id, err := parseUUIDParam(c, "id")
	if err != nil {
		return err
	}
	filter := domain.AgentTaskFilter{
		Status:     c.Query("status"),
		Search:     c.Query("search"),
		PageParams: page,
	}
	items, total, err := h.agentSvc.ListAgentTasks(c.Context(), id, filter)
	if err != nil {
		return err
	}
	return sendPage(c, items, total, page)
}

func (h *AgentHandler) ResumeAgent(c fiber.Ctx) error {
	id, err := parseUUIDParam(c, "id")
	if err != nil {
		return err
	}
	agent, err := h.agentSvc.ResumeAgent(c.Context(), id, currentUser(c), c.IP())
	if err != nil {
		return err
	}
	return c.JSON(agent)
}

func (h *AgentHandler) RegisterAgent(c fiber.Ctx) error {
	var req service.RegisterAgentRequest
	if err := c.Bind().Body(&req); err != nil {
		return sendError(c, fiber.StatusBadRequest, "bad_request", "invalid agent registration payload")
	}
	res, err := h.agentSvc.RegisterAgent(c.Context(), req, currentUser(c), c.IP())
	if err != nil {
		return err
	}
	return c.Status(fiber.StatusCreated).JSON(res)
}

func (h *AgentHandler) DrainAgent(c fiber.Ctx) error {
	id, err := parseUUIDParam(c, "id")
	if err != nil {
		return err
	}
	agent, err := h.agentSvc.DrainAgent(c.Context(), id, currentUser(c), c.IP())
	if err != nil {
		return err
	}
	return c.JSON(agent)
}

func (h *AgentHandler) RotateToken(c fiber.Ctx) error {
	id, err := parseUUIDParam(c, "id")
	if err != nil {
		return err
	}
	res, err := h.agentSvc.RotateToken(c.Context(), id, currentUser(c), c.IP())
	if err != nil {
		return err
	}
	return c.JSON(res)
}

func (h *AgentHandler) DeleteAgent(c fiber.Ctx) error {
	id, err := parseUUIDParam(c, "id")
	if err != nil {
		return err
	}
	if err := h.agentSvc.DeleteAgent(c.Context(), id, currentUser(c), c.IP()); err != nil {
		return err
	}
	return c.JSON(fiber.Map{"ok": true})
}

func (h *AgentHandler) Heartbeat(c fiber.Ctx) error {
	agent := currentAgent(c)
	if agent == nil {
		return domain.ErrUnauthorized
	}
	var in service.AgentHeartbeatInput
	_ = c.Bind().Body(&in)
	res, err := h.agentSvc.Heartbeat(c.Context(), agent, in)
	if err != nil {
		return err
	}
	return c.JSON(res)
}

func (h *AgentHandler) ClaimTask(c fiber.Ctx) error {
	agent := currentAgent(c)
	if agent == nil {
		return domain.ErrUnauthorized
	}
	task, err := h.agentSvc.ClaimTask(c.Context(), agent)
	if err != nil {
		return err
	}
	if task == nil {
		return c.SendStatus(fiber.StatusNoContent)
	}
	return c.JSON(task)
}

func (h *AgentHandler) CompleteTask(c fiber.Ctx) error {
	agent := currentAgent(c)
	if agent == nil {
		return domain.ErrUnauthorized
	}
	taskID, err := parseUUIDParam(c, "id")
	if err != nil {
		return err
	}
	var req struct {
		Status       domain.AgentTaskStatus `json:"status"`
		ErrorMessage string                 `json:"error_message"`
	}
	if err := c.Bind().Body(&req); err != nil {
		return sendError(c, fiber.StatusBadRequest, "bad_request", "invalid task completion payload")
	}
	if err := h.agentSvc.CompleteTask(c.Context(), agent, taskID, req.Status, req.ErrorMessage); err != nil {
		return err
	}
	return c.JSON(fiber.Map{"ok": true})
}

func (h *AgentHandler) IngestTaskLogs(c fiber.Ctx) error {
	agent := currentAgent(c)
	if agent == nil {
		return domain.ErrUnauthorized
	}
	taskID, err := parseUUIDParam(c, "id")
	if err != nil {
		return err
	}
	var req struct {
		Entries []domain.RunnerLogEntry `json:"entries"`
	}
	if err := c.Bind().Body(&req); err != nil {
		return sendError(c, fiber.StatusBadRequest, "bad_request", "invalid task log payload")
	}
	if err := h.agentSvc.IngestTaskLogs(c.Context(), agent, taskID, req.Entries); err != nil {
		return err
	}
	return c.JSON(fiber.Map{"ok": true, "accepted": len(req.Entries)})
}
