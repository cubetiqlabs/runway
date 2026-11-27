package http

import (
	"github.com/cubetiqlabs/runway/internal/domain"
	"github.com/cubetiqlabs/runway/internal/service"
	"github.com/gofiber/fiber/v3"
)

type AgentHandler struct {
	agentSvc *service.AgentService
}

func NewAgentHandler(agentSvc *service.AgentService) *AgentHandler {
	return &AgentHandler{agentSvc: agentSvc}
}

func (h *AgentHandler) ListAgents(c fiber.Ctx) error {
	if h.agentSvc == nil {
		return c.JSON(fiber.Map{"items": []domain.Agent{}, "total": 0})
	}
	items, total, err := h.agentSvc.ListAgents(c.Context(), parsePage(c))
	if err != nil {
		return err
	}
	return c.JSON(fiber.Map{"items": items, "total": total})
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
