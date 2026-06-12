package seed

import (
	"log"

	"gorm.io/gorm"

	"venue-booking-admin/internal/auth"
	"venue-booking-admin/internal/models"
)

// Run 初始化内置管理员与种子业务数据（幂等）。
func Run(database *gorm.DB, adminUser, adminPass string) error {
	var count int64
	database.Model(&models.User{}).Where("username = ?", adminUser).Count(&count)
	if count == 0 {
		hash, err := auth.HashPassword(adminPass)
		if err != nil {
			return err
		}
		database.Create(&models.User{Username: adminUser, PasswordHash: hash, DisplayName: "平台管理员"})
		log.Println("已创建管理员账号")
	}

	var venueCount int64
	database.Model(&models.Venue{}).Count(&venueCount)
	if venueCount > 0 {
		return nil
	}

	venues := []models.Venue{
		{Name: "城北全民健身中心篮球馆", SportType: "basketball", Capacity: 200, HourlyPrice: 160, OpenHour: 8, CloseHour: 22, Status: "open"},
		{Name: "奥体中心游泳馆", SportType: "swimming", Capacity: 400, HourlyPrice: 80, OpenHour: 6, CloseHour: 21, Status: "open"},
		{Name: "市民广场羽毛球馆", SportType: "badminton", Capacity: 60, HourlyPrice: 50, OpenHour: 9, CloseHour: 22, Status: "maintenance"},
		{Name: "滨江足球公园", SportType: "football", Capacity: 500, HourlyPrice: 300, OpenHour: 8, CloseHour: 20, Status: "open"},
	}
	if err := database.Create(&venues).Error; err != nil {
		return err
	}

	bookings := []models.Booking{
		{VenueID: venues[0].ID, CustomerName: "陈刚", Phone: "13700001111", BookDate: "2026-06-20", StartHour: 18, EndHour: 20, Amount: 320, Status: "booked"},
		{VenueID: venues[0].ID, CustomerName: "周敏", Phone: "13700002222", BookDate: "2026-06-20", StartHour: 20, EndHour: 21, Amount: 160, Status: "booked"},
		{VenueID: venues[1].ID, CustomerName: "黄磊", Phone: "13700003333", BookDate: "2026-06-21", StartHour: 7, EndHour: 9, Amount: 160, Status: "completed"},
		{VenueID: venues[3].ID, CustomerName: "吴静", Phone: "13700004444", BookDate: "2026-06-22", StartHour: 15, EndHour: 17, Amount: 600, Status: "cancelled"},
	}
	if err := database.Create(&bookings).Error; err != nil {
		return err
	}

	log.Println("种子数据初始化完成")
	return nil
}
