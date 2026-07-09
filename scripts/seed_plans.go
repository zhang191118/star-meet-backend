package main

import (
	"log"
	"star-meet-backend/model/database"

	"gorm.io/driver/mysql"
	"gorm.io/gorm"
)

func main() {
	dsn := "root:ServBay.dev@tcp(127.0.0.1:3306)/star_meet?charset=utf8mb4&parseTime=True&loc=Local"
	db, err := gorm.Open(mysql.Open(dsn), &gorm.Config{})
	if err != nil {
		log.Fatalf("Failed to connect to MySQL: %v", err)
	}
	log.Println("Connected to MySQL database.")

	plans := []database.Plan{
		{
			PlanID:         "monthly_pro",
			Name:           "Monthly Pro",
			Description:    "Unlimited AI Chat & Voice Messages",
			Level:          "pro",
			AppleProductID: "com.starmeet.pro.monthly",
			Price:          9.99,
			Currency:       "USD",
			Duration:       30,
		},
		{
			PlanID:         "yearly_pro",
			Name:           "Yearly Pro",
			Description:    "Best Value: Unlimited AI Chat & Voice Messages",
			Level:          "pro",
			AppleProductID: "com.starmeet.pro.yearly",
			Price:          99.99,
			Currency:       "USD",
			Duration:       365,
		},
	}

	if err := db.AutoMigrate(&database.Plan{}); err != nil {
		log.Fatalf("Failed to migrate plans table: %v", err)
	}

	for _, plan := range plans {
		var existing database.Plan
		if err := db.Where("plan_id = ?", plan.PlanID).First(&existing).Error; err == nil {
			log.Printf("Plan %s already exists, skipping...", plan.Name)
		} else {
			if err := db.Create(&plan).Error; err != nil {
				log.Printf("Failed to create plan %s: %v", plan.Name, err)
			} else {
				log.Printf("Created plan: %s", plan.Name)
			}
		}
	}

	log.Println("Seeding completed successfully!")
}
