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

	"gorm.io/gorm"
)

const (
	appleCurrencyUser  = "11111111-1111-4111-8111-111111111111"
	appleCurrencyOther = "22222222-2222-4222-8222-222222222222"
)

func setupAppleCurrencyPurchaseTest(t *testing.T) (*CurrencyService, *gorm.DB) {
	t.Helper()
	db := setupSubscriptionServiceTestDB(t)
	if err := db.AutoMigrate(
		&database.UserCurrency{},
		&database.CurrencyTransaction{},
		&database.RechargePackage{},
	); err != nil {
		t.Fatalf("migrate currency tables: %v", err)
	}
	createSubscriptionTestUser(t, db, appleCurrencyUser)
	createSubscriptionTestUser(t, db, appleCurrencyOther)
	if err := db.Create(&database.RechargePackage{
		PackageID: "star_60",
		Name:      "60星星",
		Currency:  database.CurrencyStar,
		Amount:    60,
		Price:     6,
		IsActive:  true,
	}).Error; err != nil {
		t.Fatalf("create currency package: %v", err)
	}
	return NewCurrencyService(db), db
}

func appleCurrencyJWSTransaction(transactionID, productID, environment, accountToken string) *jwsTransactionPayload {
	return &jwsTransactionPayload{
		TransactionID:         transactionID,
		OriginalTransactionID: transactionID,
		BundleID:              appleBundleID(),
		ProductID:             productID,
		Environment:           environment,
		Type:                  "Consumable",
		AppAccountToken:       accountToken,
		SignedDate:            time.Now().UnixMilli(),
	}
}

func stubAppleCurrencyJWSTransactions(t *testing.T, transactions map[string]*jwsTransactionPayload) {
	t.Helper()
	stubAppleJWSVerifiers(t, func(signed string) (*jwsTransactionPayload, error) {
		transaction, ok := transactions[signed]
		if !ok {
			return nil, fmt.Errorf("unexpected signed transaction: %s", signed)
		}
		return transaction, nil
	}, nil)
}

func stubAppleCurrencyHTTPClient(t *testing.T, roundTrip appleHTTPRoundTripFunc) {
	t.Helper()
	originalClient := appleCurrencyHTTPClient
	t.Cleanup(func() { appleCurrencyHTTPClient = originalClient })
	appleCurrencyHTTPClient = &http.Client{
		Timeout:   20 * time.Second,
		Transport: roundTrip,
	}
}

func appleCurrencyHTTPResponse(t *testing.T, req *http.Request, status int, signed string) *http.Response {
	t.Helper()
	body := `{}`
	if signed != "" {
		encoded, err := json.Marshal(appleCurrencyTransactionResponse{SignedTransactionInfo: signed})
		if err != nil {
			t.Fatalf("marshal transaction response: %v", err)
		}
		body = string(encoded)
	}
	return &http.Response{
		StatusCode: status,
		Header:     make(http.Header),
		Body:       io.NopCloser(strings.NewReader(body)),
		Request:    req,
	}
}

func stubAppleCurrencyReceipt(t *testing.T, response func(payload map[string]interface{}) (*appleVerifyReceiptResponse, error)) {
	t.Helper()
	original := appleVerifyReceiptRequestFunc
	t.Cleanup(func() { appleVerifyReceiptRequestFunc = original })
	appleVerifyReceiptRequestFunc = func(_ string, payload map[string]interface{}) (*appleVerifyReceiptResponse, error) {
		return response(payload)
	}
}

func TestAppleCurrencyExactTransactionCreditsConsecutivePurchasesAndReplayOnce(t *testing.T) {
	service, db := setupAppleCurrencyPurchaseTest(t)
	stubAppleServerAPIConfirmation(t, "server-token")
	productID := appleCoinProductPrefix + "star_60"
	stubAppleCurrencyJWSTransactions(t, map[string]*jwsTransactionPayload{
		"signed-1001": appleCurrencyJWSTransaction("1001", productID, "Production", appleCurrencyUser),
		"signed-1002": appleCurrencyJWSTransaction("1002", productID, "Production", appleCurrencyUser),
	})
	requested := make([]string, 0, 3)
	stubAppleCurrencyHTTPClient(t, func(req *http.Request) (*http.Response, error) {
		requested = append(requested, req.URL.String())
		transactionID := strings.TrimPrefix(req.URL.Path, "/inApps/v1/transactions/")
		return appleCurrencyHTTPResponse(t, req, http.StatusOK, "signed-"+transactionID), nil
	})

	for _, transactionID := range []string{"1001", "1002"} {
		balance, idempotent, err := service.ConfirmAppleCurrencyPurchase(appleCurrencyUser, AppleCurrencyPurchaseConfirmation{
			PackageID:     "star_60",
			TransactionID: transactionID,
		})
		if err != nil {
			t.Fatalf("confirm transaction %s: %v", transactionID, err)
		}
		if idempotent || balance[database.CurrencyStar] != int64(len(requested)*60) {
			t.Fatalf("unexpected balance after %s: balance=%v idempotent=%v", transactionID, balance, idempotent)
		}
	}
	balance, idempotent, err := service.ConfirmAppleCurrencyPurchase(appleCurrencyUser, AppleCurrencyPurchaseConfirmation{
		PackageID:     "star_60",
		TransactionID: "1002",
	})
	if err != nil {
		t.Fatalf("replay transaction: %v", err)
	}
	if !idempotent || balance[database.CurrencyStar] != 120 {
		t.Fatalf("replay changed balance: balance=%v idempotent=%v", balance, idempotent)
	}
	if len(requested) != 3 ||
		requested[0] != appStoreAPIProduction+"/inApps/v1/transactions/1001" ||
		requested[1] != appStoreAPIProduction+"/inApps/v1/transactions/1002" {
		t.Fatalf("unexpected Server API requests: %#v", requested)
	}

	var transactions int64
	if err := db.Model(&database.CurrencyTransaction{}).
		Where("user_id = ? AND type = ?", appleCurrencyUser, database.TransactionRecharge).
		Count(&transactions).Error; err != nil {
		t.Fatal(err)
	}
	if transactions != 2 {
		t.Fatalf("expected two credited purchases, got %d", transactions)
	}
	var owners int64
	if err := db.Model(&database.AppleTransactionOwner{}).
		Where("user_id = ? AND environment = ?", appleCurrencyUser, "Production").
		Count(&owners).Error; err != nil {
		t.Fatal(err)
	}
	if owners != 2 {
		t.Fatalf("expected two transaction owners, got %d", owners)
	}
}

func TestAppleCurrencyExactTransactionFallsBackOnlyOnProduction404(t *testing.T) {
	service, db := setupAppleCurrencyPurchaseTest(t)
	stubAppleServerAPIConfirmation(t, "server-token")
	productID := appleCoinProductPrefix + "star_60"
	stubAppleCurrencyJWSTransactions(t, map[string]*jwsTransactionPayload{
		"signed-sandbox": appleCurrencyJWSTransaction("2001", productID, "Sandbox", appleCurrencyUser),
	})
	requests := make([]string, 0, 2)
	stubAppleCurrencyHTTPClient(t, func(req *http.Request) (*http.Response, error) {
		requests = append(requests, req.URL.String())
		if strings.HasPrefix(req.URL.String(), appStoreAPIProduction) {
			return appleCurrencyHTTPResponse(t, req, http.StatusNotFound, ""), nil
		}
		return appleCurrencyHTTPResponse(t, req, http.StatusOK, "signed-sandbox"), nil
	})

	balance, _, err := service.ConfirmAppleCurrencyPurchase(appleCurrencyUser, AppleCurrencyPurchaseConfirmation{
		PackageID:     "star_60",
		TransactionID: "2001",
	})
	if err != nil {
		t.Fatalf("confirm sandbox transaction: %v", err)
	}
	if balance[database.CurrencyStar] != 60 {
		t.Fatalf("expected 60 stars, got %v", balance)
	}
	if len(requests) != 2 ||
		requests[1] != appStoreAPISandbox+"/inApps/v1/transactions/2001" {
		t.Fatalf("unexpected fallback requests: %#v", requests)
	}
	var owner database.AppleTransactionOwner
	if err := db.Where("original_transaction_id = ?", "2001").First(&owner).Error; err != nil {
		t.Fatal(err)
	}
	if owner.Environment != "Sandbox" {
		t.Fatalf("expected sandbox ownership, got %+v", owner)
	}
}

func TestAppleCurrencyExactTransactionDoesNotFallbackOnUnauthorized(t *testing.T) {
	service, _ := setupAppleCurrencyPurchaseTest(t)
	stubAppleServerAPIConfirmation(t, "server-token")
	requests := 0
	stubAppleCurrencyHTTPClient(t, func(req *http.Request) (*http.Response, error) {
		requests++
		return appleCurrencyHTTPResponse(t, req, http.StatusUnauthorized, ""), nil
	})

	_, _, err := service.ConfirmAppleCurrencyPurchase(appleCurrencyUser, AppleCurrencyPurchaseConfirmation{
		PackageID:     "star_60",
		TransactionID: "2002",
	})
	if err == nil {
		t.Fatal("expected authorization failure")
	}
	if requests != 1 {
		t.Fatalf("production 401 must not fall back, got %d requests", requests)
	}
}

func TestAppleCurrencyExactTransactionRejectsCrossUserOwner(t *testing.T) {
	service, db := setupAppleCurrencyPurchaseTest(t)
	stubAppleServerAPIConfirmation(t, "server-token")
	productID := appleCoinProductPrefix + "star_60"
	stubAppleCurrencyJWSTransactions(t, map[string]*jwsTransactionPayload{
		"signed-3001": appleCurrencyJWSTransaction("3001", productID, "Production", ""),
	})
	stubAppleCurrencyHTTPClient(t, func(req *http.Request) (*http.Response, error) {
		return appleCurrencyHTTPResponse(t, req, http.StatusOK, "signed-3001"), nil
	})
	params := AppleCurrencyPurchaseConfirmation{PackageID: "star_60", TransactionID: "3001"}
	if _, _, err := service.ConfirmAppleCurrencyPurchase(appleCurrencyUser, params); err != nil {
		t.Fatalf("first owner purchase: %v", err)
	}
	if _, _, err := service.ConfirmAppleCurrencyPurchase(appleCurrencyOther, params); !errors.Is(err, ErrAppleCurrencyReceiptAlreadyUsed) {
		t.Fatalf("expected cross-user ownership conflict, got %v", err)
	}

	firstBalance, err := service.GetUserBalance(appleCurrencyUser)
	if err != nil {
		t.Fatal(err)
	}
	otherBalance, err := service.GetUserBalance(appleCurrencyOther)
	if err != nil {
		t.Fatal(err)
	}
	if firstBalance[database.CurrencyStar] != 60 || otherBalance[database.CurrencyStar] != 0 {
		t.Fatalf("cross-user replay changed balances: first=%v other=%v", firstBalance, otherBalance)
	}
	var transactions int64
	if err := db.Model(&database.CurrencyTransaction{}).
		Where("client_order_id = ?", "3001").
		Count(&transactions).Error; err != nil {
		t.Fatal(err)
	}
	if transactions != 1 {
		t.Fatalf("expected one global transaction row, got %d", transactions)
	}
}

func TestAppleCurrencyExactTransactionRejectsRevokedWithoutCredit(t *testing.T) {
	service, db := setupAppleCurrencyPurchaseTest(t)
	stubAppleServerAPIConfirmation(t, "server-token")
	productID := appleCoinProductPrefix + "star_60"
	transaction := appleCurrencyJWSTransaction("4001", productID, "Production", appleCurrencyUser)
	transaction.RevocationDate = time.Now().UnixMilli()
	stubAppleCurrencyJWSTransactions(t, map[string]*jwsTransactionPayload{"signed-revoked": transaction})
	stubAppleCurrencyHTTPClient(t, func(req *http.Request) (*http.Response, error) {
		return appleCurrencyHTTPResponse(t, req, http.StatusOK, "signed-revoked"), nil
	})

	_, _, err := service.ConfirmAppleCurrencyPurchase(appleCurrencyUser, AppleCurrencyPurchaseConfirmation{
		PackageID:     "star_60",
		TransactionID: "4001",
	})
	if !errors.Is(err, ErrAppleReceiptRevoked) {
		t.Fatalf("expected revoked transaction, got %v", err)
	}
	var transactions int64
	if err := db.Model(&database.CurrencyTransaction{}).Count(&transactions).Error; err != nil {
		t.Fatal(err)
	}
	if transactions != 0 {
		t.Fatalf("revoked transaction created %d rows", transactions)
	}
}

func TestAppleCurrencyExactTransactionRejectsMismatchedAppleProof(t *testing.T) {
	productID := appleCoinProductPrefix + "star_60"
	tests := []struct {
		name        string
		transaction *jwsTransactionPayload
	}{
		{
			name:        "transaction id",
			transaction: appleCurrencyJWSTransaction("5002", productID, "Production", appleCurrencyUser),
		},
		{
			name:        "product",
			transaction: appleCurrencyJWSTransaction("5001", appleCoinProductPrefix+"star_300", "Production", appleCurrencyUser),
		},
		{
			name: "bundle",
			transaction: func() *jwsTransactionPayload {
				transaction := appleCurrencyJWSTransaction("5001", productID, "Production", appleCurrencyUser)
				transaction.BundleID = "com.example.other"
				return transaction
			}(),
		},
		{
			name:        "environment",
			transaction: appleCurrencyJWSTransaction("5001", productID, "Sandbox", appleCurrencyUser),
		},
		{
			name: "type",
			transaction: func() *jwsTransactionPayload {
				transaction := appleCurrencyJWSTransaction("5001", productID, "Production", appleCurrencyUser)
				transaction.Type = "Non-Consumable"
				return transaction
			}(),
		},
		{
			name:        "account token",
			transaction: appleCurrencyJWSTransaction("5001", productID, "Production", appleCurrencyOther),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			service, db := setupAppleCurrencyPurchaseTest(t)
			stubAppleServerAPIConfirmation(t, "server-token")
			stubAppleCurrencyJWSTransactions(t, map[string]*jwsTransactionPayload{"signed": tt.transaction})
			stubAppleCurrencyHTTPClient(t, func(req *http.Request) (*http.Response, error) {
				return appleCurrencyHTTPResponse(t, req, http.StatusOK, "signed"), nil
			})

			_, _, err := service.ConfirmAppleCurrencyPurchase(appleCurrencyUser, AppleCurrencyPurchaseConfirmation{
				PackageID:     "star_60",
				TransactionID: "5001",
			})
			if !errors.Is(err, ErrInvalidAppleCurrencyReceipt) {
				t.Fatalf("expected mismatch rejection, got %v", err)
			}
			var transactions int64
			if err := db.Model(&database.CurrencyTransaction{}).Count(&transactions).Error; err != nil {
				t.Fatal(err)
			}
			if transactions != 0 {
				t.Fatalf("mismatched proof created %d transactions", transactions)
			}
		})
	}
}

func TestAppleCurrencyWithoutCredentialsRequiresExactReceiptNotClientJWS(t *testing.T) {
	service, db := setupAppleCurrencyPurchaseTest(t)
	originalConfigured := appleServerAPICredentialsConfiguredFunc
	t.Cleanup(func() { appleServerAPICredentialsConfiguredFunc = originalConfigured })
	appleServerAPICredentialsConfiguredFunc = func() bool { return false }

	productID := appleCoinProductPrefix + "star_60"
	stubAppleCurrencyJWSTransactions(t, map[string]*jwsTransactionPayload{
		"signed-client": appleCurrencyJWSTransaction("6001", productID, "Sandbox", appleCurrencyUser),
	})
	stubAppleCurrencyReceipt(t, func(map[string]interface{}) (*appleVerifyReceiptResponse, error) {
		return &appleVerifyReceiptResponse{
			Status:      0,
			Environment: "Sandbox",
			Receipt: appleReceiptPayload{
				BundleID: appleBundleID(),
				InApp: []appleReceiptItem{
					{
						ProductID:      productID,
						TransactionID:  "6100",
						RevocationDate: "1700000000000",
					},
					{
						ProductID:        productID,
						TransactionID:    "6001",
						AppAccountToken:  appleCurrencyUser,
						RevocationDateMs: "0",
					},
				},
			},
		}, nil
	})

	_, _, err := service.ConfirmAppleCurrencyPurchase(appleCurrencyUser, AppleCurrencyPurchaseConfirmation{
		PackageID:     "star_60",
		TransactionID: "6001",
		JWS:           "signed-client",
	})
	if err == nil {
		t.Fatal("client JWS alone must not authorize a transaction without Server API credentials")
	}

	balance, idempotent, err := service.ConfirmAppleCurrencyPurchase(appleCurrencyUser, AppleCurrencyPurchaseConfirmation{
		PackageID:     "star_60",
		TransactionID: "6001",
		JWS:           "signed-client",
		Receipt:       "dGVzdA==",
		IsSandbox:     true,
	})
	if err != nil {
		t.Fatalf("confirm exact receipt transaction: %v", err)
	}
	if idempotent || balance[database.CurrencyStar] != 60 {
		t.Fatalf("unexpected exact receipt result: balance=%v idempotent=%v", balance, idempotent)
	}
	var transaction database.CurrencyTransaction
	if err := db.Where("client_order_id = ?", "6001").First(&transaction).Error; err != nil {
		t.Fatal(err)
	}
	if transaction.UserID != appleCurrencyUser {
		t.Fatalf("unexpected receipt owner: %+v", transaction)
	}
}

func TestLegacyAppleCurrencyReceiptSelectsLatestUncreditedTransaction(t *testing.T) {
	service, db := setupAppleCurrencyPurchaseTest(t)
	originalConfigured := appleServerAPICredentialsConfiguredFunc
	t.Cleanup(func() { appleServerAPICredentialsConfiguredFunc = originalConfigured })
	appleServerAPICredentialsConfiguredFunc = func() bool { return false }

	productID := appleCoinProductPrefix + "star_60"
	items := []appleReceiptItem{{
		ProductID:       productID,
		TransactionID:   "7001",
		AppAccountToken: appleCurrencyUser,
	}}
	stubAppleCurrencyReceipt(t, func(map[string]interface{}) (*appleVerifyReceiptResponse, error) {
		return &appleVerifyReceiptResponse{
			Status:      0,
			Environment: "Sandbox",
			Receipt: appleReceiptPayload{
				BundleID: appleBundleID(),
				InApp:    append([]appleReceiptItem(nil), items...),
			},
		}, nil
	})

	if _, _, err := service.VerifyAppleReceiptAndRecharge(appleCurrencyUser, "star_60", "dGVzdA==", true); err != nil {
		t.Fatalf("first receipt purchase: %v", err)
	}
	items = append(items, appleReceiptItem{
		ProductID:       productID,
		TransactionID:   "7002",
		AppAccountToken: appleCurrencyUser,
	})
	if balance, _, err := service.VerifyAppleReceiptAndRecharge(appleCurrencyUser, "star_60", "dGVzdA==", true); err != nil {
		t.Fatalf("second receipt purchase: %v", err)
	} else if balance[database.CurrencyStar] != 120 {
		t.Fatalf("expected second purchase to credit once, got %v", balance)
	}
	balance, idempotent, err := service.VerifyAppleReceiptAndRecharge(appleCurrencyUser, "star_60", "dGVzdA==", true)
	if err != nil {
		t.Fatalf("receipt replay: %v", err)
	}
	if !idempotent || balance[database.CurrencyStar] != 120 {
		t.Fatalf("receipt replay changed balance: balance=%v idempotent=%v", balance, idempotent)
	}
	var transactions []database.CurrencyTransaction
	if err := db.Where("user_id = ?", appleCurrencyUser).Order("id ASC").Find(&transactions).Error; err != nil {
		t.Fatal(err)
	}
	if len(transactions) != 2 ||
		transactions[0].ClientOrderID == nil || *transactions[0].ClientOrderID != "7001" ||
		transactions[1].ClientOrderID == nil || *transactions[1].ClientOrderID != "7002" {
		t.Fatalf("unexpected receipt transactions: %+v", transactions)
	}
}

func TestLegacyAppleCurrencyReceiptSkipsOldRevokedCandidate(t *testing.T) {
	service, _ := setupAppleCurrencyPurchaseTest(t)
	originalConfigured := appleServerAPICredentialsConfiguredFunc
	t.Cleanup(func() { appleServerAPICredentialsConfiguredFunc = originalConfigured })
	appleServerAPICredentialsConfiguredFunc = func() bool { return false }

	productID := appleCoinProductPrefix + "star_60"
	stubAppleCurrencyReceipt(t, func(map[string]interface{}) (*appleVerifyReceiptResponse, error) {
		return &appleVerifyReceiptResponse{
			Status:      0,
			Environment: "Sandbox",
			Receipt: appleReceiptPayload{
				BundleID: appleBundleID(),
				InApp: []appleReceiptItem{
					{
						ProductID:          productID,
						TransactionID:      "7101",
						CancellationDateMs: "1700000000000",
						RevocationDateMs:   "0",
					},
					{
						ProductID:       productID,
						TransactionID:   "7102",
						AppAccountToken: appleCurrencyUser,
						RevocationDate:  "0",
					},
				},
			},
		}, nil
	})

	balance, _, err := service.VerifyAppleReceiptAndRecharge(appleCurrencyUser, "star_60", "dGVzdA==", true)
	if err != nil {
		t.Fatalf("valid transaction after revoked history: %v", err)
	}
	if balance[database.CurrencyStar] != 60 {
		t.Fatalf("expected valid transaction credit, got %v", balance)
	}
}

func TestLegacyAppleCurrencyReceiptRejectsBundleMismatch(t *testing.T) {
	service, db := setupAppleCurrencyPurchaseTest(t)
	originalConfigured := appleServerAPICredentialsConfiguredFunc
	t.Cleanup(func() { appleServerAPICredentialsConfiguredFunc = originalConfigured })
	appleServerAPICredentialsConfiguredFunc = func() bool { return false }

	stubAppleCurrencyReceipt(t, func(map[string]interface{}) (*appleVerifyReceiptResponse, error) {
		return &appleVerifyReceiptResponse{
			Status:      0,
			Environment: "Sandbox",
			Receipt: appleReceiptPayload{
				BundleID: "com.example.other",
				InApp: []appleReceiptItem{{
					ProductID:       appleCoinProductPrefix + "star_60",
					TransactionID:   "7151",
					AppAccountToken: appleCurrencyUser,
				}},
			},
		}, nil
	})

	if _, _, err := service.VerifyAppleReceiptAndRecharge(appleCurrencyUser, "star_60", "dGVzdA==", true); !errors.Is(err, ErrInvalidAppleCurrencyReceipt) {
		t.Fatalf("expected bundle mismatch rejection, got %v", err)
	}
	var transactions int64
	if err := db.Model(&database.CurrencyTransaction{}).Count(&transactions).Error; err != nil {
		t.Fatal(err)
	}
	if transactions != 0 {
		t.Fatalf("bundle mismatch created %d transactions", transactions)
	}
}

func TestLegacyAppleCurrencyReceiptRejectsHistoricalCrossUserTransaction(t *testing.T) {
	service, db := setupAppleCurrencyPurchaseTest(t)
	originalConfigured := appleServerAPICredentialsConfiguredFunc
	t.Cleanup(func() { appleServerAPICredentialsConfiguredFunc = originalConfigured })
	appleServerAPICredentialsConfiguredFunc = func() bool { return false }

	productID := appleCoinProductPrefix + "star_60"
	stubAppleCurrencyReceipt(t, func(map[string]interface{}) (*appleVerifyReceiptResponse, error) {
		return &appleVerifyReceiptResponse{
			Status:      0,
			Environment: "Sandbox",
			Receipt: appleReceiptPayload{
				BundleID: appleBundleID(),
				InApp: []appleReceiptItem{{
					ProductID:     productID,
					TransactionID: "7201",
				}},
			},
		}, nil
	})
	legacyOrderID := "7201"
	if err := db.Create(&database.CurrencyTransaction{
		UserID:        appleCurrencyOther,
		Currency:      database.CurrencyStar,
		Amount:        60,
		Type:          database.TransactionRecharge,
		RelatedID:     "star_60",
		ClientOrderID: &legacyOrderID,
	}).Error; err != nil {
		t.Fatalf("create historical transaction: %v", err)
	}

	if _, _, err := service.VerifyAppleReceiptAndRecharge(appleCurrencyUser, "star_60", "dGVzdA==", true); !errors.Is(err, ErrAppleCurrencyReceiptAlreadyUsed) {
		t.Fatalf("expected historical cross-user rejection, got %v", err)
	}
	balance, err := service.GetUserBalance(appleCurrencyUser)
	if err != nil {
		t.Fatal(err)
	}
	if balance[database.CurrencyStar] != 0 {
		t.Fatalf("cross-user receipt credited current user: %v", balance)
	}
}
