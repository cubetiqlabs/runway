package http

import (
	"github.com/cubetiqlabs/runway/internal/service"
	"github.com/gofiber/fiber/v3"
)

type WebhookHandler struct {
	svc *service.WebhookService
}

func NewWebhookHandler(svc *service.WebhookService) *WebhookHandler {
	return &WebhookHandler{svc: svc}
}

func (h *WebhookHandler) HandleGitHub(c fiber.Ctx) error {
	in := service.WebhookInput{
		DeliveryID:   c.Get("X-GitHub-Delivery"),
		EventName:    c.Get("X-GitHub-Event"),
		Signature256: c.Get("X-Hub-Signature-256"),
		RawBody:      c.Body(),
	}
	if err := h.svc.HandleGitHubWebhook(c.Context(), in); err != nil {
		return err
	}
	return c.Status(fiber.StatusAccepted).JSON(fiber.Map{
		"accepted":    true,
		"delivery_id": in.DeliveryID,
		"event":       in.EventName,
	})
}
