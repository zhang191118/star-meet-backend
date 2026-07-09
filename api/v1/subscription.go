package v1

import (
	"errors"
	"net/http"

	"star-meet-backend/global"
	"star-meet-backend/model/response"
	"star-meet-backend/service"

	"github.com/gin-gonic/gin"
)

type SubscriptionAPI struct {
	SubscriptionService *service.SubscriptionService
}

func NewSubscriptionAPI() *SubscriptionAPI {
	return &SubscriptionAPI{
		SubscriptionService: service.NewSubscriptionService(global.GVA_DB),
	}
}

func (a *SubscriptionAPI) GetPlans(c *gin.Context) {
	plans, err := a.SubscriptionService.GetPlans()
	if err != nil {
		response.Fail(c, http.StatusInternalServerError, err.Error())
		return
	}
	response.OkWithData(c, plans)
}

func (a *SubscriptionAPI) VerifyAppleReceipt(c *gin.Context) {
	var req struct {
		Receipt   string `json:"receipt" binding:"required"`
		PlanID    string `json:"plan_id" binding:"required"`
		IsSandbox bool   `json:"is_sandbox"`
	}

	if err := c.ShouldBindJSON(&req); err != nil {
		failBindError(c, err, req)
		return
	}

	userID := c.GetString("user_id")
	if userID == "" {
		failUnauthenticated(c)
		return
	}

	verification, err := a.SubscriptionService.VerifyAppleReceipt(req.Receipt, req.PlanID, req.IsSandbox)
	if err != nil {
		if errors.Is(err, service.ErrInvalidReceiptFormat) {
			response.FailWithError(c, http.StatusUnprocessableEntity, "validation failed", "validation_failed", gin.H{
				"field":  "receipt",
				"reason": "invalid_base64",
			})
			return
		}
		response.Fail(c, http.StatusBadRequest, "Invalid receipt: "+err.Error())
		return
	}

	sub, err := a.SubscriptionService.CreateSubscription(userID, req.PlanID, verification.Environment, verification.OriginalTransactionID)
	if err != nil {
		response.Fail(c, http.StatusInternalServerError, "Failed to create subscription: "+err.Error())
		return
	}

	response.OkWithMessageAndData(c, "Subscription successful", sub)
}

func (a *SubscriptionAPI) GetMySubscription(c *gin.Context) {
	userID := c.GetString("user_id")
	if userID == "" {
		failUnauthenticated(c)
		return
	}

	sub, err := a.SubscriptionService.GetActiveSubscription(userID)
	if err != nil {
		response.OkWithMessage(c, "No active subscription")
		return
	}

	response.OkWithData(c, sub)
}

func (a *SubscriptionAPI) CancelSubscription(c *gin.Context) {
	userID := c.GetString("user_id")
	if userID == "" {
		failUnauthenticated(c)
		return
	}

	if err := a.SubscriptionService.CancelSubscription(userID); err != nil {
		response.Fail(c, http.StatusInternalServerError, err.Error())
		return
	}

	response.OkWithMessage(c, "Subscription cancelled successfully")
}
