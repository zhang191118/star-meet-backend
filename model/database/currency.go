package database

import (
	"time"
)

type CurrencyType string

const (
	CurrencyStar CurrencyType = "star" // 星星 - 基础货币
	CurrencyMoon CurrencyType = "moon" // 月亮 - 抽卡货币
	CurrencySun  CurrencyType = "sun"  // 太阳 - 高级货币
)

type TransactionType string

const (
	TransactionRecharge TransactionType = "recharge" // 充值
	TransactionGift     TransactionType = "gift"     // 送礼
	TransactionGacha    TransactionType = "gacha"    // 抽卡
	TransactionRefund   TransactionType = "refund"   // 退款
	TransactionSystem   TransactionType = "system"   // 系统赠送
)

type UserCurrency struct {
	ID        uint      `json:"id" gorm:"primarykey"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`

	UserID   string       `json:"user_id" gorm:"uniqueIndex:idx_user_currency;size:64;not null"`
	Currency CurrencyType `json:"currency" gorm:"uniqueIndex:idx_user_currency;size:20;not null"`
	Balance  int64        `json:"balance" gorm:"default:0"`
}

func (UserCurrency) TableName() string {
	return "user_currencies"
}

type CurrencyTransaction struct {
	ID        uint      `json:"id" gorm:"primarykey;index:idx_currency_transactions_user_created,sort:desc,priority:3"`
	CreatedAt time.Time `json:"created_at" gorm:"index:idx_currency_transactions_user_created,sort:desc,priority:2"`

	UserID        string          `json:"user_id" gorm:"index;index:idx_currency_transactions_user_created,priority:1;uniqueIndex:idx_currency_client_order,where:client_order_id IS NOT NULL;size:64;not null"`
	Currency      CurrencyType    `json:"currency" gorm:"size:20;not null"`
	Amount        int64           `json:"amount"` // 正数增加，负数减少
	Type          TransactionType `json:"type" gorm:"uniqueIndex:idx_currency_client_order,where:client_order_id IS NOT NULL;size:20;not null"`
	RelatedID     string          `json:"related_id" gorm:"size:64"` // 订单ID等
	ClientOrderID *string         `json:"client_order_id,omitempty" gorm:"uniqueIndex:idx_currency_client_order,where:client_order_id IS NOT NULL;size:96"`
	Description   string          `json:"description" gorm:"size:255"`
}

func (CurrencyTransaction) TableName() string {
	return "currency_transactions"
}

type RechargePackage struct {
	ID        uint      `json:"id" gorm:"primarykey"`
	CreatedAt time.Time `json:"created_at"`

	PackageID   string       `json:"package_id" gorm:"uniqueIndex;size:50;not null"`
	Name        string       `json:"name" gorm:"size:50;not null"`
	Description string       `json:"description" gorm:"size:255"`
	Currency    CurrencyType `json:"currency" gorm:"size:20;not null"`
	Amount      int64        `json:"amount"` // 获得的货币数量
	Price       float64      `json:"price"`  // 价格(人民币)
	IsActive    bool         `json:"is_active" gorm:"default:true;index:idx_recharge_packages_active_sort,priority:1"`
	SortOrder   int          `json:"sort_order" gorm:"default:0;index:idx_recharge_packages_active_sort,priority:2"`
}

func (RechargePackage) TableName() string {
	return "recharge_packages"
}
