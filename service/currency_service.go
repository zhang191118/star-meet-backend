package service

import (
	"errors"
	"star-meet-backend/model/database"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

var ErrCurrencyIdempotencyConflict = errors.New("currency idempotency conflict")

type CurrencyService struct {
	DB *gorm.DB
}

func NewCurrencyService(db *gorm.DB) *CurrencyService {
	return &CurrencyService{DB: db}
}

func (s *CurrencyService) GetUserBalance(userID string) (map[database.CurrencyType]int64, error) {
	var currencies []database.UserCurrency
	if err := s.DB.Where("user_id = ?", userID).Find(&currencies).Error; err != nil {
		return nil, err
	}

	balance := map[database.CurrencyType]int64{
		database.CurrencyStar: 0,
		database.CurrencyMoon: 0,
		database.CurrencySun:  0,
	}

	for _, c := range currencies {
		balance[c.Currency] = c.Balance
	}

	return balance, nil
}

func (s *CurrencyService) GetUserCurrency(userID string, currencyType database.CurrencyType) (*database.UserCurrency, error) {
	var currency database.UserCurrency
	err := s.DB.Where("user_id = ? AND currency = ?", userID, currencyType).First(&currency).Error
	if err == gorm.ErrRecordNotFound {
		// 创建初始货币
		currency = database.UserCurrency{
			UserID:   userID,
			Currency: currencyType,
			Balance:  0,
		}
		if err := s.DB.Create(&currency).Error; err != nil {
			return nil, err
		}
		return &currency, nil
	}
	return &currency, err
}

func (s *CurrencyService) AddCurrency(userID string, currencyType database.CurrencyType, amount int64, txType database.TransactionType, relatedID, description string) error {
	return s.DB.Transaction(func(tx *gorm.DB) error {
		_, err := addCurrencyInTx(tx, userID, currencyType, amount, txType, relatedID, description, nil)
		return err
	})
}

func (s *CurrencyService) AddCurrencyWithClientOrderID(userID string, currencyType database.CurrencyType, amount int64, txType database.TransactionType, relatedID, description, clientOrderID string) (*database.CurrencyTransaction, bool, error) {
	if clientOrderID == "" {
		var created *database.CurrencyTransaction
		err := s.DB.Transaction(func(tx *gorm.DB) error {
			var err error
			created, err = addCurrencyInTx(tx, userID, currencyType, amount, txType, relatedID, description, nil)
			return err
		})
		return created, false, err
	}

	var result database.CurrencyTransaction
	idempotent := false
	err := s.DB.Transaction(func(tx *gorm.DB) error {
		var existing database.CurrencyTransaction
		err := tx.Where("user_id = ? AND type = ? AND client_order_id = ?", userID, txType, clientOrderID).First(&existing).Error
		if err == nil {
			if existing.Currency != currencyType || existing.Amount != amount || existing.RelatedID != relatedID {
				return ErrCurrencyIdempotencyConflict
			}
			result = existing
			idempotent = true
			return nil
		}
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}

		clientOrderIDCopy := clientOrderID
		created, err := addCurrencyInTx(tx, userID, currencyType, amount, txType, relatedID, description, &clientOrderIDCopy)
		if err != nil {
			return err
		}
		result = *created
		return nil
	})
	if err != nil {
		return nil, false, err
	}
	return &result, idempotent, nil
}

func addCurrencyInTx(tx *gorm.DB, userID string, currencyType database.CurrencyType, amount int64, txType database.TransactionType, relatedID, description string, clientOrderID *string) (*database.CurrencyTransaction, error) {
	// 获取或创建用户货币
	var currency database.UserCurrency
	err := tx.Where("user_id = ? AND currency = ?", userID, currencyType).First(&currency).Error
	if err == gorm.ErrRecordNotFound {
		currency = database.UserCurrency{
			UserID:   userID,
			Currency: currencyType,
			Balance:  0,
		}
		if err := tx.Create(&currency).Error; err != nil {
			return nil, err
		}
	} else if err != nil {
		return nil, err
	}

	// 更新余额
	currency.Balance += amount
	if err := tx.Save(&currency).Error; err != nil {
		return nil, err
	}

	// 创建交易记录
	transaction := database.CurrencyTransaction{
		UserID:        userID,
		Currency:      currencyType,
		Amount:        amount,
		Type:          txType,
		RelatedID:     relatedID,
		ClientOrderID: clientOrderID,
		Description:   description,
	}
	if err := tx.Create(&transaction).Error; err != nil {
		return nil, err
	}
	return &transaction, nil
}

func (s *CurrencyService) DeductCurrency(userID string, currencyType database.CurrencyType, amount int64, txType database.TransactionType, relatedID, description string) error {
	return s.DB.Transaction(func(tx *gorm.DB) error {
		var currency database.UserCurrency
		err := tx.Where("user_id = ? AND currency = ?", userID, currencyType).First(&currency).Error
		if err != nil {
			return err
		}

		if currency.Balance < amount {
			return gorm.ErrRecordNotFound
		}

		currency.Balance -= amount
		if err := tx.Save(&currency).Error; err != nil {
			return err
		}

		transaction := database.CurrencyTransaction{
			UserID:      userID,
			Currency:    currencyType,
			Amount:      -amount,
			Type:        txType,
			RelatedID:   relatedID,
			Description: description,
		}
		return tx.Create(&transaction).Error
	})
}

func (s *CurrencyService) GetTransactions(userID string, limit int) ([]database.CurrencyTransaction, error) {
	if limit <= 0 {
		limit = 50
	}
	transactions, _, err := s.GetTransactionsPage(userID, 1, limit)
	return transactions, err
}

func (s *CurrencyService) GetTransactionsPage(userID string, page, pageSize int) ([]database.CurrencyTransaction, int64, error) {
	if page <= 0 {
		page = 1
	}
	if pageSize <= 0 {
		pageSize = 20
	}
	pageSize = clampPageSize(pageSize)

	query := s.DB.Model(&database.CurrencyTransaction{}).Where("user_id = ?", userID)
	var total int64
	if err := query.Count(&total).Error; err != nil {
		return nil, 0, err
	}

	offset := (page - 1) * pageSize
	var transactions []database.CurrencyTransaction
	if err := query.Order("created_at DESC, id DESC").Offset(offset).Limit(pageSize).Find(&transactions).Error; err != nil {
		return nil, 0, err
	}
	return transactions, total, nil
}

func (s *CurrencyService) GetPackages() ([]database.RechargePackage, error) {
	if err := s.EnsureDefaultRechargePackages(); err != nil {
		return nil, err
	}

	var packages []database.RechargePackage
	if err := s.DB.Where("is_active = ?", true).Order("sort_order ASC").Find(&packages).Error; err != nil {
		return nil, err
	}

	return packages, nil
}

func (s *CurrencyService) EnsureDefaultRechargePackages() error {
	return s.DB.Transaction(func(tx *gorm.DB) error {
		var total int64
		if err := tx.Model(&database.RechargePackage{}).Count(&total).Error; err != nil {
			return err
		}
		if total > 0 {
			return nil
		}

		defaultPackages := s.getDefaultPackages()
		for i := range defaultPackages {
			if err := tx.Clauses(clause.OnConflict{
				Columns:   []clause.Column{{Name: "package_id"}},
				DoNothing: true,
			}).Create(&defaultPackages[i]).Error; err != nil {
				return err
			}
		}
		return nil
	})
}

func (s *CurrencyService) getDefaultPackages() []database.RechargePackage {
	return []database.RechargePackage{
		{PackageID: "star_60", Name: "60星星", Description: "¥6.00", Currency: database.CurrencyStar, Amount: 60, Price: 6.0, IsActive: true, SortOrder: 1},
		{PackageID: "star_300", Name: "300星星", Description: "¥30.00", Currency: database.CurrencyStar, Amount: 300, Price: 30.0, IsActive: true, SortOrder: 2},
		{PackageID: "star_980", Name: "980星星", Description: "¥98.00", Currency: database.CurrencyStar, Amount: 980, Price: 98.0, IsActive: true, SortOrder: 3},
		{PackageID: "star_1980", Name: "1980星星", Description: "¥198.00", Currency: database.CurrencyStar, Amount: 1980, Price: 198.0, IsActive: true, SortOrder: 4},
		{PackageID: "moon_300", Name: "300月亮", Description: "¥30.00", Currency: database.CurrencyMoon, Amount: 300, Price: 30.0, IsActive: true, SortOrder: 5},
		{PackageID: "moon_980", Name: "980月亮", Description: "¥98.00", Currency: database.CurrencyMoon, Amount: 980, Price: 98.0, IsActive: true, SortOrder: 6},
		{PackageID: "moon_1980", Name: "1980月亮", Description: "¥198.00", Currency: database.CurrencyMoon, Amount: 1980, Price: 198.0, IsActive: true, SortOrder: 7},
		{PackageID: "sun_60", Name: "60太阳", Description: "¥68.00", Currency: database.CurrencySun, Amount: 60, Price: 68.0, IsActive: true, SortOrder: 8},
		{PackageID: "sun_300", Name: "300太阳", Description: "¥328.00", Currency: database.CurrencySun, Amount: 300, Price: 328.0, IsActive: true, SortOrder: 9},
	}
}
