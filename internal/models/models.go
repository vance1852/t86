package models

import "time"

// User 后台用户（本平台仅 admin 一个管理员角色）。
type User struct {
	ID           uint      `gorm:"primaryKey" json:"id"`
	Username     string    `gorm:"size:64;uniqueIndex" json:"username"`
	PasswordHash string    `gorm:"size:255" json:"-"`
	DisplayName  string    `gorm:"size:64" json:"display_name"`
	CreatedAt    time.Time `json:"created_at"`
}

// Venue 体育场馆。
type Venue struct {
	ID          uint      `gorm:"primaryKey" json:"id"`
	Name        string    `gorm:"size:128" json:"name"`
	SportType   string    `gorm:"size:32" json:"sport_type"` // basketball / football / badminton / swimming ...
	Capacity    int       `json:"capacity"`
	HourlyPrice float64   `json:"hourly_price"`
	OpenHour    int       `json:"open_hour"`  // 开放起始小时，0-23
	CloseHour   int       `json:"close_hour"` // 关闭小时，1-24
	Status      string    `gorm:"size:16" json:"status"` // open / closed / maintenance
	CreatedAt   time.Time `json:"created_at"`
}

// Booking 场地预订。
type Booking struct {
	ID           uint      `gorm:"primaryKey" json:"id"`
	VenueID      uint      `gorm:"index" json:"venue_id"`
	CustomerName string    `gorm:"size:64" json:"customer_name"`
	Phone        string    `gorm:"size:32" json:"phone"`
	BookDate     string    `gorm:"size:10;index" json:"book_date"` // YYYY-MM-DD
	StartHour    int       `json:"start_hour"`
	EndHour      int       `json:"end_hour"`
	Amount       float64   `json:"amount"`
	Status       string    `gorm:"size:16" json:"status"` // booked / cancelled / completed
	CreatedAt    time.Time `json:"created_at"`
}
