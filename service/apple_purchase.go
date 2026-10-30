package service

import (
	"errors"
	"fmt"
	"log"
	"strings"
)

// ApplePurchaseConfirmation contains the proof supplied by the client. A JWS
// identifies a transaction, while the receipt is the fallback proof used when
// no App Store Server API credentials are configured.
type ApplePurchaseConfirmation struct {
	Receipt       string
	PlanID        string
	TransactionID string
	JWS           string
	IsSandbox     bool
}

// ConfirmApplePurchase requires a live Apple response before returning the
// entitlement that may be written to the database. Client-supplied JWS data is
// used only to identify and cross-check the transaction; it never controls
// expiry, renewal, or revocation state.
func (s *SubscriptionService) ConfirmApplePurchase(params ApplePurchaseConfirmation) (*AppleReceiptVerification, error) {
	planID := strings.TrimSpace(params.PlanID)
	if planID == "" {
		return nil, errors.New("缺少订阅计划")
	}
	plan, err := s.GetPlan(planID)
	if err != nil {
		return nil, err
	}

	var localTransaction *jwsTransactionPayload
	if signedJWS := strings.TrimSpace(params.JWS); signedJWS != "" {
		payload, _, err := s.verifyAppleJWSIdentity(signedJWS, planID)
		if err != nil {
			// A bad signature cannot be trusted for identity and may fall back
			// to Apple's online proof. A correctly signed but inconsistent
			// transaction is rejected without trying a weaker path.
			if errors.Is(err, ErrAppleJWSRejected) || errors.Is(err, ErrAppleReceiptRevoked) {
				return nil, err
			}
			log.Printf("[apple-debug] JWS 无法用于交易身份核验，改用在线验证: %v", err)
		} else {
			localTransaction = payload
		}
	}

	originalTransactionID := strings.TrimSpace(params.TransactionID)
	if localTransaction != nil {
		localOriginalTransactionID := strings.TrimSpace(localTransaction.OriginalTransactionID)
		if originalTransactionID != "" && originalTransactionID != localOriginalTransactionID {
			return nil, fmt.Errorf("%w: 请求交易号与 JWS 不一致", ErrAppleJWSRejected)
		}
		originalTransactionID = localOriginalTransactionID
	}

	var verification *AppleReceiptVerification
	// App Store Server API's subscription-status endpoint does not cover
	// non-consumable lifetime products. Historical lifetime restores therefore
	// always require a live receipt check, even when Server API credentials exist.
	if plan.Level == "lifetime" {
		if strings.TrimSpace(params.Receipt) == "" {
			return nil, errors.New("终身会员恢复需要 Apple 收据")
		}
		verification, err = s.VerifyAppleReceipt(params.Receipt, planID, params.IsSandbox)
		if err != nil {
			return nil, err
		}
	} else if appleServerAPICredentialsConfiguredFunc() && originalTransactionID != "" {
		// Once Server API credentials are configured, a query failure is final.
		// Falling back to an older receipt could re-grant a revoked transaction.
		verification, err = s.VerifyViaServerAPI(originalTransactionID, planID)
		if err != nil {
			return nil, err
		}
	} else {
		if strings.TrimSpace(params.Receipt) == "" {
			if originalTransactionID != "" {
				return nil, errors.New("App Store Server API 凭据未配置，且缺少可在线验证的收据")
			}
			return nil, errors.New("缺少 Apple 收据或交易号")
		}
		verification, err = s.VerifyAppleReceipt(params.Receipt, planID, params.IsSandbox)
		if err != nil {
			return nil, err
		}
	}

	if verification == nil {
		return nil, errors.New("Apple 未返回有效交易")
	}
	onlineOriginalTransactionID := strings.TrimSpace(verification.OriginalTransactionID)
	if onlineOriginalTransactionID == "" {
		return nil, errors.New("Apple 未返回原始交易号")
	}
	if originalTransactionID != "" && onlineOriginalTransactionID != originalTransactionID {
		return nil, fmt.Errorf("%w: Apple 在线交易号与客户端交易不一致", ErrAppleJWSRejected)
	}

	var appAccountToken string
	if localTransaction != nil {
		localEnvironment, err := normalizeAppleEnvironment(localTransaction.Environment)
		if err != nil {
			return nil, fmt.Errorf("%w: 未知交易环境: %s", ErrAppleJWSRejected, localTransaction.Environment)
		}
		if localEnvironment != verification.Environment {
			return nil, fmt.Errorf("%w: 客户端与 Apple 返回的交易环境不一致", ErrAppleJWSRejected)
		}

		localToken := strings.TrimSpace(localTransaction.AppAccountToken)
		onlineToken := strings.TrimSpace(verification.AppAccountToken)
		if localToken != "" && onlineToken != "" && !strings.EqualFold(localToken, onlineToken) {
			return nil, fmt.Errorf("%w: 客户端与 Apple 返回的账户令牌不一致", ErrAppleJWSRejected)
		}
		appAccountToken = onlineToken
		if appAccountToken == "" {
			appAccountToken = localToken
		}
	} else {
		appAccountToken = strings.TrimSpace(verification.AppAccountToken)
	}

	return &AppleReceiptVerification{
		Environment:           verification.Environment,
		OriginalTransactionID: onlineOriginalTransactionID,
		ExpiresAt:             verification.ExpiresAt,
		AppAccountToken:       appAccountToken,
	}, nil
}
