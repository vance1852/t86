package db

import (
	"log"
	"time"

	"gorm.io/driver/mysql"
	"gorm.io/gorm"

	"venue-booking-admin/internal/models"
)

// Connect 连接数据库，带重试以等待 MySQL 就绪。
func Connect(dsn string) (*gorm.DB, error) {
	var database *gorm.DB
	var err error
	for i := 0; i < 60; i++ {
		database, err = gorm.Open(mysql.Open(dsn), &gorm.Config{})
		if err == nil {
			sqlDB, e := database.DB()
			if e == nil {
				if e = sqlDB.Ping(); e == nil {
					log.Println("数据库已就绪")
					return database, nil
				}
			}
		}
		log.Printf("等待数据库... (%d) %v", i+1, err)
		time.Sleep(2 * time.Second)
	}
	return nil, err
}

// Migrate 自动建表。
func Migrate(database *gorm.DB) error {
	return database.AutoMigrate(&models.User{}, &models.Venue{}, &models.Booking{})
}
