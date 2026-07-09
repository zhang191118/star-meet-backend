package database

import (
	"time"
)

type Subscription struct {
	ID        uint       `json:"id" gorm:"primarykey"`
	CreatedAt time.Time  `json:"created_at"`
	UpdatedAt time.Time  `json:"updated_at"`
	DeletedAt *time.Time `json:"-" gorm:"index"`

	UserID                     string    `json:"user_id" gorm:"index;index:idx_subscriptions_user_status_end_time,priority:1;size:64;not null"`
	PlanID                     string    `json:"plan_id" gorm:"index;size:64;not null"`
	StartTime                  time.Time `json:"start_time"`
	EndTime                    time.Time `json:"end_time" gorm:"index:idx_subscriptions_user_status_end_time,sort:desc,priority:3"`
	Status                     string    `json:"status" gorm:"size:20;default:active;index:idx_subscriptions_user_status_end_time,priority:2"`
	Environment                string    `json:"environment" gorm:"size:20"`
	AutoRenewStatus            bool      `json:"auto_renew_status" gorm:"default:false"`
	AppleOriginalTransactionID string    `json:"apple_original_transaction_id" gorm:"index;size:255"`
	CancelReason               string    `json:"cancel_reason" gorm:"size:255"`

	User User `json:"user" gorm:"foreignKey:UserID;references:UserID"`
	Plan Plan `json:"plan" gorm:"foreignKey:PlanID;references:PlanID"`
}

func (Subscription) TableName() string {
	return "subscriptions"
}

type Plan struct {
	ID        uint       `json:"id" gorm:"primarykey"`
	CreatedAt time.Time  `json:"created_at"`
	UpdatedAt time.Time  `json:"updated_at"`
	DeletedAt *time.Time `json:"-" gorm:"index"`

	PlanID         string  `json:"plan_id" gorm:"uniqueIndex;size:64;not null"`
	Name           string  `json:"name" gorm:"size:100;not null"`
	Description    string  `json:"description" gorm:"size:255"`
	Level          string  `json:"level" gorm:"size:20;not null"`
	AppleProductID string  `json:"apple_product_id" gorm:"uniqueIndex;size:100;not null"`
	Price          float64 `json:"price" gorm:"type:decimal(10,2);not null"`
	Currency       string  `json:"currency" gorm:"size:3;default:USD"`
	Duration       int     `json:"duration" gorm:"comment:days"`
}

func (Plan) TableName() string {
	return "plans"
}
