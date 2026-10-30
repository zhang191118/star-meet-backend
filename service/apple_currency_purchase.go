package service

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"

	"star-meet-backend/model/database"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const appleCoinProductPrefix = "com.guoliang.starmeet.coin."

var appleCurrencyHTTPClient = &http.Client{Timeout: 20 * time.Second}

type AppleCurrencyPurchaseConfirmation struct {
	Receipt       string
	PackageID     string
	TransactionID string
	JWS           string
	IsSandbox     bool
}

type appleCurrencyPurchaseProof struct {
	TransactionIDs []string
	Environment    string
}

type appleCurrencyTransactionResponse struct {
	SignedTransactionInfo string `json:"signedTransactionInfo"`
}

// ConfirmAppleCurrencyPurchase credits one App Store consumable. Modern
// clients provide the exact transaction ID; the production response is the
// grant authority and a client-supplied JWS is only cross-checked against it.
func (s *CurrencyService) ConfirmAppleCurrencyPurchase(userID string, params AppleCurrencyPurchaseConfirmation) (map[database.CurrencyType]int64, bool, error) {
	userID = strings.TrimSpace(userID)
	packageID := strings.TrimSpace(params.PackageID)
	transactionID := strings.TrimSpace(params.TransactionID)
	if userID == "" {
		return nil, false, errors.New("缺少用户")
	}
	if packageID == "" {
		return nil, false, fmt.Errorf("recharge package unavailable")
	}

	pkg, err := s.appleCurrencyPackage(packageID)
	if err != nil {
		return nil, false, err
	}
	productID := appleCoinProductPrefix + pkg.PackageID

	var proof appleCurrencyPurchaseProof
	if transactionID != "" {
		if _, err := strconv.ParseUint(transactionID, 10, 64); err != nil {
			return nil, false, fmt.Errorf("%w: 交易号格式错误", ErrInvalidAppleCurrencyReceipt)
		}
		proof, err = s.verifyModernAppleCurrencyPurchase(userID, productID, transactionID, params.JWS, params.Receipt, params.IsSandbox)
	} else {
		proof, err = verifyLegacyAppleCurrencyReceipt(userID, productID, params.Receipt, params.IsSandbox)
	}
	if err != nil {
		return nil, false, err
	}

	balance, idempotent, err := s.creditAppleCurrencyPurchase(userID, pkg, proof)
	if err != nil {
		return nil, false, err
	}
	return balance, idempotent, nil
}

func (s *CurrencyService) appleCurrencyPackage(packageID string) (*database.RechargePackage, error) {
	packages, err := s.GetPackages()
	if err != nil {
		return nil, err
	}
	for index := range packages {
		if packages[index].PackageID == packageID {
			return &packages[index], nil
		}
	}
	return nil, fmt.Errorf("recharge package unavailable")
}

func (s *CurrencyService) verifyModernAppleCurrencyPurchase(userID, productID, transactionID, clientJWS, receipt string, isSandbox bool) (appleCurrencyPurchaseProof, error) {
	var local *jwsTransactionPayload
	var err error
	if strings.TrimSpace(clientJWS) != "" {
		local, err = appleJWSTransactionVerifier(strings.TrimSpace(clientJWS))
		if err != nil {
			return appleCurrencyPurchaseProof{}, fmt.Errorf("%w: 客户端 JWS 无效", ErrInvalidAppleCurrencyReceipt)
		}
		if err := validateAppleCurrencyTransaction(userID, productID, transactionID, "", local); err != nil {
			return appleCurrencyPurchaseProof{}, err
		}
	}

	if appleServerAPICredentialsConfiguredFunc() {
		online, environment, err := fetchAppleCurrencyTransaction(transactionID)
		if err != nil {
			return appleCurrencyPurchaseProof{}, err
		}
		if err := validateAppleCurrencyTransaction(userID, productID, transactionID, environment, online); err != nil {
			return appleCurrencyPurchaseProof{}, err
		}
		if local != nil {
			localEnvironment, err := normalizeAppleEnvironment(local.Environment)
			if err != nil || localEnvironment != environment {
				return appleCurrencyPurchaseProof{}, fmt.Errorf("%w: 客户端与 Apple 返回的交易环境不一致", ErrInvalidAppleCurrencyReceipt)
			}
		}
		return appleCurrencyPurchaseProof{
			TransactionIDs: []string{transactionID},
			Environment:    environment,
		}, nil
	}

	if strings.TrimSpace(receipt) == "" {
		return appleCurrencyPurchaseProof{}, fmt.Errorf("App Store Server API 凭据未配置，且缺少可精确匹配交易号的收据")
	}
	proof, err := verifyAppleCurrencyReceipt(userID, productID, receipt, isSandbox, transactionID)
	if err != nil {
		return appleCurrencyPurchaseProof{}, err
	}
	if local != nil {
		localEnvironment, envErr := normalizeAppleEnvironment(local.Environment)
		if envErr != nil || localEnvironment != proof.Environment {
			return appleCurrencyPurchaseProof{}, fmt.Errorf("%w: 客户端与收据的交易环境不一致", ErrInvalidAppleCurrencyReceipt)
		}
	}
	return proof, nil
}

func fetchAppleCurrencyTransaction(transactionID string) (*jwsTransactionPayload, string, error) {
	token, err := appleServerAPITokenFunc()
	if err != nil {
		return nil, "", err
	}

	environment := "Production"
	signed, statusCode, err := requestAppleCurrencyTransaction(appStoreAPIProduction, token, transactionID)
	if err != nil && statusCode == http.StatusNotFound {
		environment = "Sandbox"
		signed, statusCode, err = requestAppleCurrencyTransaction(appStoreAPISandbox, token, transactionID)
	}
	if err != nil {
		return nil, "", err
	}
	if signed == "" {
		return nil, "", errors.New("Apple 未返回 signedTransactionInfo")
	}
	transaction, err := appleJWSTransactionVerifier(signed)
	if err != nil {
		return nil, "", fmt.Errorf("验证 Apple signedTransactionInfo 失败: %w", err)
	}
	if transaction == nil {
		return nil, "", errors.New("Apple 返回空 signedTransactionInfo")
	}
	return transaction, environment, nil
}

func requestAppleCurrencyTransaction(baseURL, token, transactionID string) (string, int, error) {
	endpoint := fmt.Sprintf("%s/inApps/v1/transactions/%s", baseURL, url.PathEscape(transactionID))
	req, err := http.NewRequest(http.MethodGet, endpoint, nil)
	if err != nil {
		return "", 0, err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Accept", "application/json")

	resp, err := appleCurrencyHTTPClient.Do(req)
	if err != nil {
		return "", 0, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", resp.StatusCode, err
	}
	if resp.StatusCode != http.StatusOK {
		return "", resp.StatusCode, fmt.Errorf("App Store Server API HTTP %d: %s", resp.StatusCode, string(body))
	}
	var parsed appleCurrencyTransactionResponse
	if err := json.Unmarshal(body, &parsed); err != nil {
		return "", resp.StatusCode, err
	}
	return strings.TrimSpace(parsed.SignedTransactionInfo), resp.StatusCode, nil
}

func validateAppleCurrencyTransaction(userID, productID, expectedTransactionID, expectedEnvironment string, transaction *jwsTransactionPayload) error {
	if transaction == nil {
		return fmt.Errorf("%w: Apple 未返回交易", ErrInvalidAppleCurrencyReceipt)
	}
	if strings.TrimSpace(transaction.TransactionID) != expectedTransactionID {
		return fmt.Errorf("%w: Apple 返回交易号与请求不一致", ErrInvalidAppleCurrencyReceipt)
	}
	if transaction.ProductID != productID {
		return fmt.Errorf("%w: Apple 返回商品不匹配", ErrInvalidAppleCurrencyReceipt)
	}
	if transaction.BundleID != appleBundleID() {
		return fmt.Errorf("%w: Apple 返回 bundleId 不匹配", ErrInvalidAppleCurrencyReceipt)
	}
	environment, err := normalizeAppleEnvironment(transaction.Environment)
	if err != nil {
		return fmt.Errorf("%w: Apple 返回未知环境", ErrInvalidAppleCurrencyReceipt)
	}
	if expectedEnvironment != "" {
		expected, err := normalizeAppleEnvironment(expectedEnvironment)
		if err != nil || expected != environment {
			return fmt.Errorf("%w: Apple 返回交易环境不一致", ErrInvalidAppleCurrencyReceipt)
		}
	}
	if !strings.EqualFold(strings.TrimSpace(transaction.Type), "consumable") {
		return fmt.Errorf("%w: Apple 返回的不是消耗型交易", ErrInvalidAppleCurrencyReceipt)
	}
	if transaction.RevocationDate > 0 {
		return fmt.Errorf("%w: Apple 交易已撤销", ErrAppleReceiptRevoked)
	}
	if transaction.SignedDate > time.Now().Add(5*time.Minute).UnixMilli() {
		return fmt.Errorf("%w: Apple 交易签名时间异常", ErrInvalidAppleCurrencyReceipt)
	}
	if strings.TrimSpace(transaction.AppAccountToken) != "" &&
		!AppleAccountTokenMatchesUser(transaction.AppAccountToken, userID) {
		return fmt.Errorf("%w: Apple 交易不属于当前账号", ErrInvalidAppleCurrencyReceipt)
	}
	return nil
}

func verifyLegacyAppleCurrencyReceipt(userID, productID, receipt string, isSandbox bool) (appleCurrencyPurchaseProof, error) {
	return verifyAppleCurrencyReceipt(userID, productID, receipt, isSandbox, "")
}

func verifyAppleCurrencyReceipt(userID, productID, receipt string, isSandbox bool, exactTransactionID string) (appleCurrencyPurchaseProof, error) {
	receipt = strings.TrimSpace(receipt)
	if _, err := base64.StdEncoding.DecodeString(receipt); err != nil {
		return appleCurrencyPurchaseProof{}, ErrInvalidAppleCurrencyReceipt
	}

	payload := map[string]interface{}{
		"receipt-data":             receipt,
		"exclude-old-transactions": false,
	}
	if sharedSecret := appleSharedSecret(); sharedSecret != "" {
		payload["password"] = sharedSecret
	}
	verifyURL := appleProductionVerifyURL
	if isSandbox {
		verifyURL = appleSandboxVerifyURL
	}
	response, err := appleVerifyReceiptRequestFunc(verifyURL, payload)
	if err != nil {
		return appleCurrencyPurchaseProof{}, err
	}
	if response == nil {
		return appleCurrencyPurchaseProof{}, fmt.Errorf("%w: Apple 返回空收据", ErrInvalidAppleCurrencyReceipt)
	}
	switch response.Status {
	case 21007:
		response, err = appleVerifyReceiptRequestFunc(appleSandboxVerifyURL, payload)
	case 21008:
		response, err = appleVerifyReceiptRequestFunc(appleProductionVerifyURL, payload)
	}
	if err != nil {
		return appleCurrencyPurchaseProof{}, err
	}
	if response == nil {
		return appleCurrencyPurchaseProof{}, fmt.Errorf("%w: Apple 返回空收据", ErrInvalidAppleCurrencyReceipt)
	}
	if response.Status != 0 {
		return appleCurrencyPurchaseProof{}, fmt.Errorf("%w: Apple 验证失败，status=%d", ErrInvalidAppleCurrencyReceipt, response.Status)
	}
	if response.Receipt.BundleID != appleBundleID() {
		return appleCurrencyPurchaseProof{}, fmt.Errorf("%w: 收据 bundleId 不匹配", ErrInvalidAppleCurrencyReceipt)
	}
	environment, err := normalizeAppleEnvironment(response.Environment)
	if err != nil {
		return appleCurrencyPurchaseProof{}, fmt.Errorf("%w: 收据环境无效", ErrInvalidAppleCurrencyReceipt)
	}

	items := append([]appleReceiptItem{}, response.Receipt.InApp...)
	items = append(items, response.LatestReceiptInfo...)
	validByID := make(map[string]appleReceiptItem, len(items))
	blockedByID := make(map[string]bool)
	revokedCandidate := false
	for _, item := range items {
		item.TransactionID = strings.TrimSpace(item.TransactionID)
		if item.TransactionID == "" || item.ProductID != productID {
			continue
		}
		if exactTransactionID != "" && item.TransactionID != exactTransactionID {
			continue
		}
		if appleReceiptItemRevoked(item) {
			if exactTransactionID != "" {
				return appleCurrencyPurchaseProof{}, fmt.Errorf("%w: 收据交易已撤销", ErrAppleReceiptRevoked)
			}
			blockedByID[item.TransactionID] = true
			revokedCandidate = true
			continue
		}
		if strings.TrimSpace(item.AppAccountToken) != "" &&
			!AppleAccountTokenMatchesUser(item.AppAccountToken, userID) {
			if exactTransactionID != "" {
				return appleCurrencyPurchaseProof{}, fmt.Errorf("%w: 收据交易不属于当前账号", ErrInvalidAppleCurrencyReceipt)
			}
			blockedByID[item.TransactionID] = true
			continue
		}
		if _, exists := validByID[item.TransactionID]; !exists {
			validByID[item.TransactionID] = item
		}
	}
	candidates := make([]appleReceiptItem, 0, len(validByID))
	for transactionID, item := range validByID {
		if !blockedByID[transactionID] {
			candidates = append(candidates, item)
		}
	}
	if len(candidates) == 0 {
		if revokedCandidate {
			return appleCurrencyPurchaseProof{}, fmt.Errorf("%w: 收据交易已撤销", ErrAppleReceiptRevoked)
		}
		return appleCurrencyPurchaseProof{}, fmt.Errorf("%w: 收据中没有该商品的交易", ErrInvalidAppleCurrencyReceipt)
	}
	sort.Slice(candidates, func(i, j int) bool {
		return appleTransactionIDGreater(candidates[i].TransactionID, candidates[j].TransactionID)
	})
	transactionIDs := make([]string, 0, len(candidates))
	for _, item := range candidates {
		transactionIDs = append(transactionIDs, item.TransactionID)
	}
	return appleCurrencyPurchaseProof{
		TransactionIDs: transactionIDs,
		Environment:    environment,
	}, nil
}

func appleReceiptItemRevoked(item appleReceiptItem) bool {
	return appleReceiptTimestampPresent(item.RevocationDate) ||
		appleReceiptTimestampPresent(item.RevocationDateMs) ||
		appleReceiptTimestampPresent(item.CancellationDate) ||
		appleReceiptTimestampPresent(item.CancellationDateMs)
}

func appleReceiptTimestampPresent(value string) bool {
	value = strings.TrimSpace(value)
	if value == "" {
		return false
	}
	if timestamp, err := strconv.ParseInt(value, 10, 64); err == nil {
		return timestamp > 0
	}
	return true
}

func appleTransactionIDGreater(left, right string) bool {
	leftID, leftErr := strconv.ParseUint(left, 10, 64)
	rightID, rightErr := strconv.ParseUint(right, 10, 64)
	if leftErr == nil && rightErr == nil {
		return leftID > rightID
	}
	return left > right
}

func (s *CurrencyService) creditAppleCurrencyPurchase(userID string, pkg *database.RechargePackage, proof appleCurrencyPurchaseProof) (map[database.CurrencyType]int64, bool, error) {
	if len(proof.TransactionIDs) == 0 {
		return nil, false, ErrInvalidAppleCurrencyReceipt
	}
	idempotent := false
	err := s.DB.Transaction(func(tx *gorm.DB) error {
		if err := lockAppleCurrencyUser(tx, userID); err != nil {
			return err
		}

		var existingTransactions []database.CurrencyTransaction
		if err := tx.Where("type = ? AND client_order_id IN ?", database.TransactionRecharge, proof.TransactionIDs).
			Find(&existingTransactions).Error; err != nil {
			return err
		}
		transactionsByID := make(map[string][]database.CurrencyTransaction)
		for _, transaction := range existingTransactions {
			if transaction.ClientOrderID == nil {
				continue
			}
			transactionsByID[*transaction.ClientOrderID] = append(transactionsByID[*transaction.ClientOrderID], transaction)
			if transaction.UserID != userID {
				return ErrAppleCurrencyReceiptAlreadyUsed
			}
		}

		var owners []database.AppleTransactionOwner
		if err := tx.Where("environment = ? AND original_transaction_id IN ?", proof.Environment, proof.TransactionIDs).
			Find(&owners).Error; err != nil {
			return err
		}
		ownerByID := make(map[string]database.AppleTransactionOwner, len(owners))
		for _, owner := range owners {
			if owner.UserID != userID {
				return ErrAppleCurrencyReceiptAlreadyUsed
			}
			ownerByID[owner.OriginalTransactionID] = owner
		}

		selectedTransactionID := ""
		idempotentTransactionID := ""
		for _, transactionID := range proof.TransactionIDs {
			transactions := transactionsByID[transactionID]
			if len(transactions) == 0 {
				selectedTransactionID = transactionID
				break
			}
			for _, transaction := range transactions {
				if transaction.Currency != pkg.Currency ||
					transaction.Amount != pkg.Amount ||
					transaction.RelatedID != pkg.PackageID {
					return ErrCurrencyIdempotencyConflict
				}
			}
			if idempotentTransactionID == "" {
				idempotentTransactionID = transactionID
			}
		}

		for transactionID := range transactionsByID {
			if err := claimAppleCurrencyOwnerInTx(tx, userID, proof.Environment, transactionID); err != nil {
				return err
			}
		}

		if selectedTransactionID == "" {
			if idempotentTransactionID == "" {
				return ErrInvalidAppleCurrencyReceipt
			}
			if _, exists := ownerByID[idempotentTransactionID]; !exists {
				if err := claimAppleCurrencyOwnerInTx(tx, userID, proof.Environment, idempotentTransactionID); err != nil {
					return err
				}
			}
			idempotent = true
			return nil
		}

		if err := claimAppleCurrencyOwnerInTx(tx, userID, proof.Environment, selectedTransactionID); err != nil {
			return err
		}
		clientOrderID := selectedTransactionID
		_, err := addCurrencyInTx(
			tx,
			userID,
			pkg.Currency,
			pkg.Amount,
			database.TransactionRecharge,
			pkg.PackageID,
			pkg.Name,
			&clientOrderID,
		)
		return err
	})
	if err != nil {
		return nil, false, err
	}
	balance, err := s.GetUserBalance(userID)
	return balance, idempotent, err
}

func lockAppleCurrencyUser(tx *gorm.DB, userID string) error {
	var user database.User
	return tx.Clauses(clause.Locking{Strength: "UPDATE"}).
		Where("user_id = ?", userID).
		First(&user).Error
}

func claimAppleCurrencyOwnerInTx(tx *gorm.DB, userID, environment, transactionID string) error {
	var owner database.AppleTransactionOwner
	err := tx.Where("environment = ? AND original_transaction_id = ?", environment, transactionID).
		First(&owner).Error
	if err == nil {
		if owner.UserID != userID {
			return ErrAppleCurrencyReceiptAlreadyUsed
		}
		return tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			Where("id = ?", owner.ID).
			First(&owner).Error
	}
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		return err
	}

	candidate := database.AppleTransactionOwner{
		Environment:           environment,
		OriginalTransactionID: transactionID,
		UserID:                userID,
	}
	if err := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&candidate).Error; err != nil {
		return err
	}
	owner = database.AppleTransactionOwner{}
	if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
		Where("environment = ? AND original_transaction_id = ?", environment, transactionID).
		First(&owner).Error; err != nil {
		return err
	}
	if owner.UserID != userID {
		return ErrAppleCurrencyReceiptAlreadyUsed
	}
	return nil
}
