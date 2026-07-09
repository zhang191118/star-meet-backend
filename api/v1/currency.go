package v1

import (
	"errors"
	"net/http"
	"star-meet-backend/global"
	"star-meet-backend/model/database"
	"star-meet-backend/model/response"
	"star-meet-backend/service"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
)

type CurrencyAPI struct {
	CurrencyService *service.CurrencyService
}

type rechargePackageResp struct {
	ID          string                `json:"id"`
	CreatedAt   time.Time             `json:"created_at"`
	PackageID   string                `json:"package_id"`
	Name        string                `json:"name"`
	Description string                `json:"description"`
	Currency    database.CurrencyType `json:"currency"`
	Amount      int64                 `json:"amount"`
	Price       float64               `json:"price"`
	IsActive    bool                  `json:"is_active"`
	SortOrder   int                   `json:"sort_order"`
}

type currencyTransactionResp struct {
	ID            string                   `json:"id"`
	CreatedAt     time.Time                `json:"created_at"`
	UserID        string                   `json:"user_id"`
	Currency      database.CurrencyType    `json:"currency"`
	Amount        int64                    `json:"amount"`
	Type          database.TransactionType `json:"type"`
	RelatedID     string                   `json:"related_id"`
	ClientOrderID *string                  `json:"client_order_id,omitempty"`
	Description   string                   `json:"description"`
}

func NewCurrencyAPI() *CurrencyAPI {
	return &CurrencyAPI{
		CurrencyService: service.NewCurrencyService(global.GVA_DB),
	}
}

func (a *CurrencyAPI) GetBalance(c *gin.Context) {
	userID := c.GetString("user_id")
	if userID == "" {
		failUnauthenticated(c)
		return
	}

	balance, err := a.CurrencyService.GetUserBalance(userID)
	if err != nil {
		response.Fail(c, http.StatusInternalServerError, err.Error())
		return
	}

	response.OkWithData(c, balance)
}

func (a *CurrencyAPI) GetPackages(c *gin.Context) {
	packages, err := a.CurrencyService.GetPackages()
	if err != nil {
		response.Fail(c, http.StatusInternalServerError, err.Error())
		return
	}

	response.OkWithData(c, toRechargePackageRespList(packages))
}

func (a *CurrencyAPI) GetTransactions(c *gin.Context) {
	userID := c.GetString("user_id")
	if userID == "" {
		failUnauthenticated(c)
		return
	}

	hasPaginationQuery := c.Query("page") != "" || c.Query("page_size") != ""
	if !hasPaginationQuery {
		transactions, err := a.CurrencyService.GetTransactions(userID, 50)
		if err != nil {
			response.Fail(c, http.StatusInternalServerError, err.Error())
			return
		}
		if transactions == nil {
			transactions = []database.CurrencyTransaction{}
		}
		response.OkWithData(c, toCurrencyTransactionRespList(transactions))
		return
	}

	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	pageSize, _ := strconv.Atoi(c.DefaultQuery("page_size", "20"))
	page, pageSize = normalizePagination(page, pageSize, 20)

	transactions, total, err := a.CurrencyService.GetTransactionsPage(userID, page, pageSize)
	if err != nil {
		response.Fail(c, http.StatusInternalServerError, err.Error())
		return
	}

	if transactions == nil {
		transactions = []database.CurrencyTransaction{}
	}

	response.OkWithData(c, gin.H{
		"list":      toCurrencyTransactionRespList(transactions),
		"total":     total,
		"page":      page,
		"page_size": pageSize,
		"has_more":  hasMore(page, pageSize, total),
	})
}

func (a *CurrencyAPI) Recharge(c *gin.Context) {
	userID := c.GetString("user_id")
	if userID == "" {
		failUnauthenticated(c)
		return
	}

	var req struct {
		PackageID     string `json:"package_id" binding:"required"`
		ClientOrderID string `json:"client_order_id"`
	}

	if err := c.ShouldBindJSON(&req); err != nil {
		failBindError(c, err, req)
		return
	}

	// 模拟支付成功，实际应该集成支付网关
	// 这里直接添加货币
	packages, err := a.CurrencyService.GetPackages()
	if err != nil {
		response.Fail(c, http.StatusInternalServerError, err.Error())
		return
	}

	var selectedPackage *database.RechargePackage
	for _, p := range packages {
		if p.PackageID == req.PackageID {
			selectedPackage = &p
			break
		}
	}

	if selectedPackage == nil {
		response.FailWithError(c, http.StatusNotFound, "recharge package unavailable", "recharge_package_unavailable", nil)
		return
	}

	responseData := gin.H{}
	if req.ClientOrderID != "" {
		transaction, idempotent, err := a.CurrencyService.AddCurrencyWithClientOrderID(userID, selectedPackage.Currency, selectedPackage.Amount, database.TransactionRecharge, req.PackageID, selectedPackage.Name, req.ClientOrderID)
		if err != nil {
			if errors.Is(err, service.ErrCurrencyIdempotencyConflict) {
				response.FailWithError(c, http.StatusConflict, "client_order_id conflicts with an existing recharge", "currency_idempotency_conflict", nil)
				return
			}
			response.Fail(c, http.StatusInternalServerError, err.Error())
			return
		}
		responseData["transaction_id"] = strconv.FormatUint(uint64(transaction.ID), 10)
		responseData["idempotent"] = idempotent
	} else {
		err = a.CurrencyService.AddCurrency(userID, selectedPackage.Currency, selectedPackage.Amount, database.TransactionRecharge, req.PackageID, selectedPackage.Name)
		if err != nil {
			response.Fail(c, http.StatusInternalServerError, err.Error())
			return
		}
	}

	balance, _ := a.CurrencyService.GetUserBalance(userID)

	responseData["balance"] = balance
	response.OkWithData(c, responseData)
}

func toRechargePackageRespList(packages []database.RechargePackage) []rechargePackageResp {
	result := make([]rechargePackageResp, 0, len(packages))
	for _, pkg := range packages {
		result = append(result, rechargePackageResp{
			ID:          pkg.PackageID,
			CreatedAt:   pkg.CreatedAt,
			PackageID:   pkg.PackageID,
			Name:        pkg.Name,
			Description: pkg.Description,
			Currency:    pkg.Currency,
			Amount:      pkg.Amount,
			Price:       pkg.Price,
			IsActive:    pkg.IsActive,
			SortOrder:   pkg.SortOrder,
		})
	}
	return result
}

func toCurrencyTransactionRespList(transactions []database.CurrencyTransaction) []currencyTransactionResp {
	result := make([]currencyTransactionResp, 0, len(transactions))
	for _, transaction := range transactions {
		result = append(result, currencyTransactionResp{
			ID:            strconv.FormatUint(uint64(transaction.ID), 10),
			CreatedAt:     transaction.CreatedAt,
			UserID:        transaction.UserID,
			Currency:      transaction.Currency,
			Amount:        transaction.Amount,
			Type:          transaction.Type,
			RelatedID:     transaction.RelatedID,
			ClientOrderID: transaction.ClientOrderID,
			Description:   transaction.Description,
		})
	}
	return result
}
