package service

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"star-meet-backend/model/database"
)

type appleHTTPRoundTripFunc func(*http.Request) (*http.Response, error)

func (f appleHTTPRoundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

func stubAppleServerAPIConfirmation(t *testing.T, token string) {
	t.Helper()
	originalConfigured := appleServerAPICredentialsConfiguredFunc
	originalToken := appleServerAPITokenFunc
	t.Cleanup(func() {
		appleServerAPICredentialsConfiguredFunc = originalConfigured
		appleServerAPITokenFunc = originalToken
	})
	appleServerAPICredentialsConfiguredFunc = func() bool { return true }
	appleServerAPITokenFunc = func() (string, error) { return token, nil }
}

func stubAppleJWSVerifiers(
	t *testing.T,
	transaction func(string) (*jwsTransactionPayload, error),
	renewal func(string) (*jwsRenewalInfo, error),
) {
	t.Helper()
	originalTransaction := appleJWSTransactionVerifier
	originalRenewal := appleJWSRenewalInfoVerifier
	t.Cleanup(func() {
		appleJWSTransactionVerifier = originalTransaction
		appleJWSRenewalInfoVerifier = originalRenewal
	})
	appleJWSTransactionVerifier = transaction
	if renewal != nil {
		appleJWSRenewalInfoVerifier = renewal
	}
}

func stubAppleServerTransport(t *testing.T, body interface{}, statusCode int) {
	t.Helper()
	encoded, err := json.Marshal(body)
	if err != nil {
		t.Fatalf("marshal Apple response: %v", err)
	}
	originalTransport := http.DefaultTransport
	t.Cleanup(func() { http.DefaultTransport = originalTransport })
	http.DefaultTransport = appleHTTPRoundTripFunc(func(req *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: statusCode,
			Header:     make(http.Header),
			Body:       io.NopCloser(strings.NewReader(string(encoded))),
			Request:    req,
		}, nil
	})
}

func appleServerTransactionPayload(environment string, expiresAt time.Time) *jwsTransactionPayload {
	return appleServerTransactionPayloadFor(
		"server-original",
		"com.guoliang.starmeet.monthly",
		environment,
		expiresAt,
	)
}

func appleServerTransactionPayloadFor(originalTransactionID, productID, environment string, expiresAt time.Time) *jwsTransactionPayload {
	return &jwsTransactionPayload{
		TransactionID:         "transaction-1",
		OriginalTransactionID: originalTransactionID,
		BundleID:              appleBundleID(),
		ProductID:             productID,
		ExpiresDate:           expiresAt.UnixMilli(),
		Environment:           environment,
		Type:                  "Auto-Renewable Subscription",
		AppAccountToken:       "11111111-1111-4111-8111-111111111111",
	}
}

func appleServerStatusResponse(environment string, lastTransactions ...subscriptionLastTransaction) *subscriptionStatusesResponse {
	return appleServerStatusResponseWithGroups(environment, subscriptionStatusGroup{
		SubscriptionGroupIdentifier: "group-1",
		LastTransactions:            lastTransactions,
	})
}

func appleServerStatusResponseWithGroups(environment string, groups ...subscriptionStatusGroup) *subscriptionStatusesResponse {
	return &subscriptionStatusesResponse{
		Environment: environment,
		BundleID:    appleBundleID(),
		Data:        groups,
	}
}

func TestVerifyViaServerAPIParsesNestedV1StatusAndUsesExactEndpoint(t *testing.T) {
	db := setupSubscriptionServiceTestDB(t)
	service := NewSubscriptionService(db)
	stubAppleServerAPIConfirmation(t, "server-token")

	expiresAt := time.Now().Add(48 * time.Hour).Truncate(time.Millisecond)
	stubAppleJWSVerifiers(t, func(signed string) (*jwsTransactionPayload, error) {
		if signed != "signed-server-transaction" {
			t.Fatalf("unexpected signed transaction: %s", signed)
		}
		return appleServerTransactionPayload("Production", expiresAt), nil
	}, nil)
	originalTransport := http.DefaultTransport
	t.Cleanup(func() { http.DefaultTransport = originalTransport })
	http.DefaultTransport = appleHTTPRoundTripFunc(func(req *http.Request) (*http.Response, error) {
		if req.Method != http.MethodGet {
			t.Fatalf("expected GET, got %s", req.Method)
		}
		if req.URL.String() != appStoreAPIProduction+"/inApps/v1/subscriptions/server-original" {
			t.Fatalf("unexpected Server API URL: %s", req.URL.String())
		}
		if req.Header.Get("Authorization") != "Bearer server-token" {
			t.Fatalf("unexpected authorization header: %s", req.Header.Get("Authorization"))
		}
		body := appleServerStatusResponse("Production", subscriptionLastTransaction{
			OriginalTransactionID: "server-original",
			TransactionID:         "transaction-1",
			Status:                1,
			SignedTransactionInfo: "signed-server-transaction",
		})
		encoded, err := json.Marshal(body)
		if err != nil {
			t.Fatalf("marshal response: %v", err)
		}
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     make(http.Header),
			Body:       io.NopCloser(strings.NewReader(string(encoded))),
			Request:    req,
		}, nil
	})

	verification, err := service.VerifyViaServerAPI("server-original", "monthly")
	if err != nil {
		t.Fatalf("verify server API status: %v", err)
	}
	if verification.Environment != "Production" ||
		verification.OriginalTransactionID != "server-original" ||
		!verification.ExpiresAt.Equal(expiresAt) ||
		verification.AppAccountToken != "11111111-1111-4111-8111-111111111111" {
		t.Fatalf("unexpected verification: %+v", verification)
	}
}

func TestConfirmApplePurchaseUsesLiveStatusInsteadOfLocalJWSExpiry(t *testing.T) {
	db := setupSubscriptionServiceTestDB(t)
	service := NewSubscriptionService(db)
	createSubscriptionTestUser(t, db, "jws-live-owner")
	stubAppleServerAPIConfirmation(t, "server-token")

	onlineExpiry := time.Now().Add(72 * time.Hour).Truncate(time.Millisecond)
	localExpiry := time.Now().Add(-time.Hour)
	stubAppleJWSVerifiers(t, func(signed string) (*jwsTransactionPayload, error) {
		switch signed {
		case "signed-local-jws":
			payload := appleServerTransactionPayload("Production", localExpiry)
			payload.AppAccountToken = "11111111-1111-4111-8111-111111111111"
			return payload, nil
		case "signed-server-transaction":
			return appleServerTransactionPayload("Production", onlineExpiry), nil
		default:
			return nil, fmt.Errorf("unexpected JWS: %s", signed)
		}
	}, nil)
	stubAppleServerTransport(t, appleServerStatusResponse("Production", subscriptionLastTransaction{
		OriginalTransactionID: "server-original",
		Status:                1,
		SignedTransactionInfo: "signed-server-transaction",
	}), http.StatusOK)

	receiptCalls := 0
	originalVerifyReceipt := appleVerifyReceiptRequestFunc
	t.Cleanup(func() { appleVerifyReceiptRequestFunc = originalVerifyReceipt })
	appleVerifyReceiptRequestFunc = func(url string, payload map[string]interface{}) (*appleVerifyReceiptResponse, error) {
		receiptCalls++
		return nil, errors.New("receipt verification must not run")
	}

	verification, err := service.ConfirmApplePurchase(ApplePurchaseConfirmation{
		PlanID:        "monthly",
		TransactionID: "server-original",
		JWS:           "signed-local-jws",
	})
	if err != nil {
		t.Fatalf("confirm purchase: %v", err)
	}
	if receiptCalls != 0 {
		t.Fatalf("Server API success fell back to verifyReceipt %d times", receiptCalls)
	}
	if !verification.ExpiresAt.Equal(onlineExpiry) {
		t.Fatalf("grant used local expiry: got %v want %v", verification.ExpiresAt, onlineExpiry)
	}

	subscription, err := service.CreateSubscription(
		"jws-live-owner",
		"monthly",
		verification.Environment,
		verification.OriginalTransactionID,
		verification.ExpiresAt,
	)
	if err != nil {
		t.Fatalf("create confirmed subscription: %v", err)
	}
	if subscription.Status != "active" || !subscription.EndTime.Equal(onlineExpiry) {
		t.Fatalf("unexpected confirmed subscription: %+v", subscription)
	}
}

func TestConfirmApplePurchaseRejectsRevokedTransactionWithoutGrant(t *testing.T) {
	db := setupSubscriptionServiceTestDB(t)
	service := NewSubscriptionService(db)
	createSubscriptionTestUser(t, db, "revoked-owner")
	stubAppleServerAPIConfirmation(t, "server-token")

	expiresAt := time.Now().Add(48 * time.Hour)
	stubAppleJWSVerifiers(t, func(string) (*jwsTransactionPayload, error) {
		return appleServerTransactionPayload("Production", expiresAt), nil
	}, nil)
	stubAppleServerTransport(t, appleServerStatusResponse("Production", subscriptionLastTransaction{
		OriginalTransactionID: "server-original",
		Status:                5,
		SignedTransactionInfo: "signed-revoked",
	}), http.StatusOK)

	_, err := service.ConfirmApplePurchase(ApplePurchaseConfirmation{
		PlanID:        "monthly",
		TransactionID: "server-original",
		JWS:           "signed-local-jws",
	})
	if !errors.Is(err, ErrAppleReceiptRevoked) {
		t.Fatalf("expected revoked transaction to fail closed, got %v", err)
	}
	var subscriptions int64
	if err := db.Model(&database.Subscription{}).Where("user_id = ?", "revoked-owner").Count(&subscriptions).Error; err != nil {
		t.Fatal(err)
	}
	if subscriptions != 0 {
		t.Fatalf("revoked transaction created %d subscriptions", subscriptions)
	}
}

func TestConfirmApplePurchaseFailsClosedWhenServerAPIIsUnavailable(t *testing.T) {
	db := setupSubscriptionServiceTestDB(t)
	service := NewSubscriptionService(db)
	stubAppleServerAPIConfirmation(t, "server-token")

	expiresAt := time.Now().Add(48 * time.Hour)
	stubAppleJWSVerifiers(t, func(string) (*jwsTransactionPayload, error) {
		return appleServerTransactionPayload("Production", expiresAt), nil
	}, nil)

	originalTransport := http.DefaultTransport
	t.Cleanup(func() { http.DefaultTransport = originalTransport })
	http.DefaultTransport = appleHTTPRoundTripFunc(func(req *http.Request) (*http.Response, error) {
		return nil, errors.New("Apple connection unavailable")
	})

	receiptCalls := 0
	originalVerifyReceipt := appleVerifyReceiptRequestFunc
	t.Cleanup(func() { appleVerifyReceiptRequestFunc = originalVerifyReceipt })
	appleVerifyReceiptRequestFunc = func(url string, payload map[string]interface{}) (*appleVerifyReceiptResponse, error) {
		receiptCalls++
		return nil, errors.New("receipt fallback must not run")
	}

	_, err := service.ConfirmApplePurchase(ApplePurchaseConfirmation{
		Receipt:       "dGVzdA==",
		PlanID:        "monthly",
		TransactionID: "server-original",
		JWS:           "signed-local-jws",
	})
	if err == nil {
		t.Fatal("expected Server API outage to reject the purchase")
	}
	if receiptCalls != 0 {
		t.Fatalf("Server API failure fell back to verifyReceipt %d times", receiptCalls)
	}
}

func TestConfirmApplePurchaseUsesReceiptWhenServerAPICredentialsAreMissing(t *testing.T) {
	db := setupSubscriptionServiceTestDB(t)
	service := NewSubscriptionService(db)

	originalConfigured := appleServerAPICredentialsConfiguredFunc
	t.Cleanup(func() { appleServerAPICredentialsConfiguredFunc = originalConfigured })
	appleServerAPICredentialsConfiguredFunc = func() bool { return false }

	expiresAt := time.Now().Add(48 * time.Hour).Truncate(time.Millisecond)
	stubAppleJWSVerifiers(t, func(string) (*jwsTransactionPayload, error) {
		return appleServerTransactionPayload("Sandbox", expiresAt), nil
	}, nil)

	receiptCalls := 0
	originalVerifyReceipt := appleVerifyReceiptRequestFunc
	t.Cleanup(func() { appleVerifyReceiptRequestFunc = originalVerifyReceipt })
	appleVerifyReceiptRequestFunc = func(url string, payload map[string]interface{}) (*appleVerifyReceiptResponse, error) {
		receiptCalls++
		return &appleVerifyReceiptResponse{
			Status:      0,
			Environment: "Sandbox",
			LatestReceiptInfo: []appleReceiptItem{{
				ProductID:             "com.guoliang.starmeet.monthly",
				OriginalTransactionID: "server-original",
				ExpiresDateMs:         fmt.Sprintf("%d", expiresAt.UnixMilli()),
				AppAccountToken:       "11111111-1111-4111-8111-111111111111",
			}},
		}, nil
	}

	verification, err := service.ConfirmApplePurchase(ApplePurchaseConfirmation{
		Receipt:   "dGVzdA==",
		PlanID:    "monthly",
		JWS:       "signed-local-jws",
		IsSandbox: true,
	})
	if err != nil {
		t.Fatalf("confirm with receipt: %v", err)
	}
	if receiptCalls != 1 {
		t.Fatalf("expected one receipt verification, got %d", receiptCalls)
	}
	if verification.Environment != "Sandbox" || !verification.ExpiresAt.Equal(expiresAt) {
		t.Fatalf("unexpected receipt verification: %+v", verification)
	}
}

func TestConfirmApplePurchaseLifetimeRestoreUsesReceiptWithServerCredentials(t *testing.T) {
	db := setupSubscriptionServiceTestDB(t)
	service := NewSubscriptionService(db)
	createSubscriptionTestUser(t, db, "lifetime-restore-owner")
	stubAppleServerAPIConfirmation(t, "server-token")
	stubAppleJWSVerifiers(t, func(string) (*jwsTransactionPayload, error) {
		return &jwsTransactionPayload{
			TransactionID:         "lifetime-transaction",
			OriginalTransactionID: "lifetime-original",
			BundleID:              appleBundleID(),
			ProductID:             "com.guoliang.starmeet.lifetime",
			Environment:           "Production",
			Type:                  "Non-Consumable",
		}, nil
	}, nil)

	receiptCalls := 0
	originalVerifyReceipt := appleVerifyReceiptRequestFunc
	t.Cleanup(func() { appleVerifyReceiptRequestFunc = originalVerifyReceipt })
	appleVerifyReceiptRequestFunc = func(url string, payload map[string]interface{}) (*appleVerifyReceiptResponse, error) {
		receiptCalls++
		return &appleVerifyReceiptResponse{
			Status:      0,
			Environment: "Production",
			LatestReceiptInfo: []appleReceiptItem{{
				ProductID:             "com.guoliang.starmeet.lifetime",
				OriginalTransactionID: "lifetime-original",
				TransactionID:         "lifetime-transaction",
				AppAccountToken:       "11111111-1111-4111-8111-111111111111",
			}},
		}, nil
	}

	serverAPICalls := 0
	originalTransport := http.DefaultTransport
	t.Cleanup(func() { http.DefaultTransport = originalTransport })
	http.DefaultTransport = appleHTTPRoundTripFunc(func(req *http.Request) (*http.Response, error) {
		serverAPICalls++
		return nil, errors.New("lifetime restore must not call the subscription-status API")
	})

	verification, err := service.ConfirmApplePurchase(ApplePurchaseConfirmation{
		Receipt:       "dGVzdA==",
		PlanID:        "lifetime",
		TransactionID: "lifetime-original",
		JWS:           "signed-lifetime-jws",
	})
	if err != nil {
		t.Fatalf("restore lifetime purchase: %v", err)
	}
	if receiptCalls != 1 {
		t.Fatalf("expected one live receipt verification, got %d", receiptCalls)
	}
	if serverAPICalls != 0 {
		t.Fatalf("lifetime restore used subscription-status API %d times", serverAPICalls)
	}
	if verification.Environment != "Production" ||
		verification.OriginalTransactionID != "lifetime-original" ||
		!verification.ExpiresAt.IsZero() {
		t.Fatalf("unexpected lifetime verification: %+v", verification)
	}

	subscription, err := service.CreateSubscription(
		"lifetime-restore-owner",
		"lifetime",
		verification.Environment,
		verification.OriginalTransactionID,
		verification.ExpiresAt,
	)
	if err != nil {
		t.Fatalf("create restored lifetime subscription: %v", err)
	}
	if subscription.PlanID != "lifetime" || subscription.Status != "active" {
		t.Fatalf("unexpected restored lifetime subscription: %+v", subscription)
	}
}

func TestConfirmApplePurchaseLifetimeRejectsCancelledReceipt(t *testing.T) {
	db := setupSubscriptionServiceTestDB(t)
	service := NewSubscriptionService(db)
	stubAppleServerAPIConfirmation(t, "server-token")

	originalVerifyReceipt := appleVerifyReceiptRequestFunc
	t.Cleanup(func() { appleVerifyReceiptRequestFunc = originalVerifyReceipt })
	appleVerifyReceiptRequestFunc = func(url string, payload map[string]interface{}) (*appleVerifyReceiptResponse, error) {
		return &appleVerifyReceiptResponse{
			Status:      0,
			Environment: "Production",
			LatestReceiptInfo: []appleReceiptItem{{
				ProductID:             "com.guoliang.starmeet.lifetime",
				OriginalTransactionID: "lifetime-cancelled-original",
				TransactionID:         "lifetime-cancelled-transaction",
				CancellationDateMs:    fmt.Sprintf("%d", time.Now().UnixMilli()),
			}},
		}, nil
	}

	_, err := service.ConfirmApplePurchase(ApplePurchaseConfirmation{
		Receipt: "dGVzdA==",
		PlanID:  "lifetime",
	})
	if !errors.Is(err, ErrAppleReceiptRevoked) {
		t.Fatalf("expected cancelled lifetime purchase to be revoked, got %v", err)
	}
}

func TestConfirmApplePurchaseRejectsMismatchedJWSAndRequestTransaction(t *testing.T) {
	db := setupSubscriptionServiceTestDB(t)
	service := NewSubscriptionService(db)

	expiresAt := time.Now().Add(48 * time.Hour)
	stubAppleJWSVerifiers(t, func(string) (*jwsTransactionPayload, error) {
		return appleServerTransactionPayload("Production", expiresAt), nil
	}, nil)

	_, err := service.ConfirmApplePurchase(ApplePurchaseConfirmation{
		PlanID:        "monthly",
		TransactionID: "different-original",
		JWS:           "signed-local-jws",
	})
	if !errors.Is(err, ErrAppleJWSRejected) {
		t.Fatalf("expected mismatched transaction to be rejected, got %v", err)
	}
}

func TestVerifyViaServerAPIDoesNotTreatAuthFailureAsSandbox(t *testing.T) {
	db := setupSubscriptionServiceTestDB(t)
	service := NewSubscriptionService(db)
	stubAppleServerAPIConfirmation(t, "server-token")

	expiresAt := time.Now().Add(48 * time.Hour)
	stubAppleJWSVerifiers(t, func(string) (*jwsTransactionPayload, error) {
		return appleServerTransactionPayload("Production", expiresAt), nil
	}, nil)

	calls := 0
	originalTransport := http.DefaultTransport
	t.Cleanup(func() { http.DefaultTransport = originalTransport })
	http.DefaultTransport = appleHTTPRoundTripFunc(func(req *http.Request) (*http.Response, error) {
		calls++
		if req.URL.Host != "api.storekit.itunes.apple.com" {
			t.Fatalf("auth failure queried a different environment: %s", req.URL.String())
		}
		return &http.Response{
			StatusCode: http.StatusUnauthorized,
			Header:     make(http.Header),
			Body:       io.NopCloser(strings.NewReader(`{"errorCode":4010000}`)),
			Request:    req,
		}, nil
	})

	if _, err := service.VerifyViaServerAPI("server-original", "monthly"); err == nil {
		t.Fatal("expected authentication failure to reject verification")
	}
	if calls != 1 {
		t.Fatalf("authentication failure made %d requests; sandbox retry should only follow a 404", calls)
	}
}

func TestVerificationFromSubscriptionStatusesSelectsOnlyRequestedOriginalAndProduct(t *testing.T) {
	now := time.Now()
	targetExpiry := now.Add(48 * time.Hour).Truncate(time.Millisecond)
	unrelatedExpiry := now.Add(72 * time.Hour).Truncate(time.Millisecond)
	stubAppleJWSVerifiers(t, func(signed string) (*jwsTransactionPayload, error) {
		switch signed {
		case "signed-unrelated-monthly":
			return appleServerTransactionPayloadFor(
				"unrelated-original",
				"com.guoliang.starmeet.monthly",
				"Production",
				unrelatedExpiry,
			), nil
		case "signed-target-monthly":
			return appleServerTransactionPayloadFor(
				"target-original",
				"com.guoliang.starmeet.monthly",
				"Production",
				targetExpiry,
			), nil
		case "signed-target-yearly":
			return appleServerTransactionPayloadFor(
				"target-original",
				"com.guoliang.starmeet.yearly",
				"Production",
				unrelatedExpiry,
			), nil
		case "signed-target-expired":
			return appleServerTransactionPayloadFor(
				"target-original",
				"com.guoliang.starmeet.monthly",
				"Production",
				now.Add(-time.Hour),
			), nil
		default:
			return nil, fmt.Errorf("unexpected signed transaction: %s", signed)
		}
	}, nil)

	t.Run("target transaction is selected from multiple groups", func(t *testing.T) {
		verification, err := verificationFromSubscriptionStatuses(
			appleServerStatusResponseWithGroups(
				"Production",
				subscriptionStatusGroup{
					SubscriptionGroupIdentifier: "unrelated-group",
					LastTransactions: []subscriptionLastTransaction{{
						OriginalTransactionID: "unrelated-original",
						Status:                1,
						SignedTransactionInfo: "signed-unrelated-monthly",
					}},
				},
				subscriptionStatusGroup{
					SubscriptionGroupIdentifier: "target-group",
					LastTransactions: []subscriptionLastTransaction{{
						OriginalTransactionID: "target-original",
						Status:                1,
						SignedTransactionInfo: "signed-target-monthly",
					}},
				},
			),
			"Production",
			"target-original",
			"com.guoliang.starmeet.monthly",
			now,
		)
		if err != nil {
			t.Fatalf("select target transaction: %v", err)
		}
		if verification.OriginalTransactionID != "target-original" || !verification.ExpiresAt.Equal(targetExpiry) {
			t.Fatalf("selected the wrong transaction: %+v", verification)
		}
	})

	t.Run("unrelated valid transaction cannot satisfy target product", func(t *testing.T) {
		_, err := verificationFromSubscriptionStatuses(
			appleServerStatusResponseWithGroups(
				"Production",
				subscriptionStatusGroup{
					SubscriptionGroupIdentifier: "unrelated-group",
					LastTransactions: []subscriptionLastTransaction{{
						OriginalTransactionID: "unrelated-original",
						Status:                1,
						SignedTransactionInfo: "signed-unrelated-monthly",
					}},
				},
				subscriptionStatusGroup{
					SubscriptionGroupIdentifier: "target-group",
					LastTransactions: []subscriptionLastTransaction{{
						OriginalTransactionID: "target-original",
						Status:                1,
						SignedTransactionInfo: "signed-target-yearly",
					}},
				},
			),
			"Production",
			"target-original",
			"com.guoliang.starmeet.monthly",
			now,
		)
		if err == nil {
			t.Fatal("unrelated valid transaction granted the requested product")
		}
	})

	t.Run("expired target cannot borrow unrelated valid transaction", func(t *testing.T) {
		_, err := verificationFromSubscriptionStatuses(
			appleServerStatusResponseWithGroups(
				"Production",
				subscriptionStatusGroup{
					SubscriptionGroupIdentifier: "unrelated-group",
					LastTransactions: []subscriptionLastTransaction{{
						OriginalTransactionID: "unrelated-original",
						Status:                1,
						SignedTransactionInfo: "signed-unrelated-monthly",
					}},
				},
				subscriptionStatusGroup{
					SubscriptionGroupIdentifier: "target-group",
					LastTransactions: []subscriptionLastTransaction{{
						OriginalTransactionID: "target-original",
						Status:                2,
						SignedTransactionInfo: "signed-target-expired",
					}},
				},
			),
			"Production",
			"target-original",
			"com.guoliang.starmeet.monthly",
			now,
		)
		if err == nil {
			t.Fatal("expired target received an entitlement from an unrelated group")
		}
	})
}

func TestVerificationFromSubscriptionStatusesGrantsOnlyActiveOrGracePeriod(t *testing.T) {
	now := time.Now()
	activeExpiry := now.Add(24 * time.Hour).Truncate(time.Millisecond)
	graceExpiry := now.Add(6 * time.Hour).Truncate(time.Millisecond)
	stubAppleJWSVerifiers(t, func(signed string) (*jwsTransactionPayload, error) {
		switch signed {
		case "signed-active":
			return appleServerTransactionPayload("Production", activeExpiry), nil
		case "signed-revoked":
			return appleServerTransactionPayload("Production", now.Add(48*time.Hour)), nil
		case "signed-retry":
			return appleServerTransactionPayload("Production", now.Add(48*time.Hour)), nil
		case "signed-expired":
			return appleServerTransactionPayload("Production", now.Add(-time.Hour)), nil
		case "signed-grace":
			return appleServerTransactionPayload("Production", now.Add(-time.Hour)), nil
		default:
			return nil, fmt.Errorf("unexpected transaction: %s", signed)
		}
	}, func(signed string) (*jwsRenewalInfo, error) {
		if signed != "signed-grace-renewal" {
			return nil, fmt.Errorf("unexpected renewal: %s", signed)
		}
		return &jwsRenewalInfo{
			OriginalTransactionID:  "server-original",
			ProductID:              "com.guoliang.starmeet.monthly",
			Environment:            "Production",
			GracePeriodExpiresDate: graceExpiry.UnixMilli(),
			IsInBillingRetryPeriod: true,
		}, nil
	})

	active, err := verificationFromSubscriptionStatuses(
		appleServerStatusResponse("Production", subscriptionLastTransaction{
			OriginalTransactionID: "server-original",
			Status:                1,
			SignedTransactionInfo: "signed-active",
		}),
		"Production",
		"server-original",
		"com.guoliang.starmeet.monthly",
		now,
	)
	if err != nil {
		t.Fatalf("active status failed: %v", err)
	}
	if !active.ExpiresAt.Equal(activeExpiry) {
		t.Fatalf("unexpected active expiry: %v", active.ExpiresAt)
	}

	grace, err := verificationFromSubscriptionStatuses(
		appleServerStatusResponse("Production", subscriptionLastTransaction{
			OriginalTransactionID: "server-original",
			Status:                4,
			SignedRenewalInfo:     "signed-grace-renewal",
			SignedTransactionInfo: "signed-grace",
		}),
		"Production",
		"server-original",
		"com.guoliang.starmeet.monthly",
		now,
	)
	if err != nil {
		t.Fatalf("grace period status failed: %v", err)
	}
	if !grace.ExpiresAt.Equal(graceExpiry) {
		t.Fatalf("grace period did not use gracePeriodExpiresDate: %v", grace.ExpiresAt)
	}

	if _, err := verificationFromSubscriptionStatuses(
		appleServerStatusResponse("Production", subscriptionLastTransaction{
			OriginalTransactionID: "server-original",
			Status:                5,
			SignedTransactionInfo: "signed-revoked",
		}),
		"Production",
		"server-original",
		"com.guoliang.starmeet.monthly",
		now,
	); !errors.Is(err, ErrAppleReceiptRevoked) {
		t.Fatalf("expected revoked status to fail, got %v", err)
	}

	if _, err := verificationFromSubscriptionStatuses(
		appleServerStatusResponse("Production", subscriptionLastTransaction{
			OriginalTransactionID: "server-original",
			Status:                3,
			SignedTransactionInfo: "signed-retry",
		}),
		"Production",
		"server-original",
		"com.guoliang.starmeet.monthly",
		now,
	); err == nil {
		t.Fatal("expected billing retry status to fail closed")
	}

	if _, err := verificationFromSubscriptionStatuses(
		appleServerStatusResponse("Production", subscriptionLastTransaction{
			OriginalTransactionID: "server-original",
			Status:                2,
			SignedTransactionInfo: "signed-expired",
		}),
		"Production",
		"server-original",
		"com.guoliang.starmeet.monthly",
		now,
	); err == nil {
		t.Fatal("expected expired status to fail closed")
	}

	if _, err := verificationFromSubscriptionStatuses(
		appleServerStatusResponse("Production", subscriptionLastTransaction{
			OriginalTransactionID: "server-original",
			Status:                1,
			SignedTransactionInfo: "signed-expired",
		}),
		"Production",
		"server-original",
		"com.guoliang.starmeet.monthly",
		now,
	); err == nil {
		t.Fatal("expected active status with a past expiry to fail closed")
	}
}
