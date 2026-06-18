package seed

import (
	"fmt"
	"log"
	"time"

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
		{VenueID: venues[0].ID, CustomerName: "陈刚", Phone: "13700001111", BookDate: "2026-06-20", StartHour: 18, EndHour: 20, Amount: 320, VenueDeposit: 200, Status: "booked"},
		{VenueID: venues[0].ID, CustomerName: "周敏", Phone: "13700002222", BookDate: "2026-06-20", StartHour: 20, EndHour: 21, Amount: 160, VenueDeposit: 200, Status: "booked"},
		{VenueID: venues[1].ID, CustomerName: "黄磊", Phone: "13700003333", BookDate: "2026-06-21", StartHour: 7, EndHour: 9, Amount: 160, VenueDeposit: 100, Status: "completed"},
		{VenueID: venues[3].ID, CustomerName: "吴静", Phone: "13700004444", BookDate: "2026-06-22", StartHour: 15, EndHour: 17, Amount: 600, VenueDeposit: 500, Status: "cancelled"},
	}
	if err := database.Create(&bookings).Error; err != nil {
		return err
	}

	// ---------- 器材类别 ----------
	categories := []models.EquipmentCategory{
		{Name: "羽毛球拍", Description: "专业碳素羽毛球拍"},
		{Name: "篮球", Description: "7号标准比赛用球"},
		{Name: "足球", Description: "5号标准比赛用球"},
		{Name: "护腕", Description: "运动护具护腕"},
		{Name: "护膝", Description: "运动护具护膝"},
		{Name: "泳镜", Description: "防雾游泳眼镜"},
		{Name: "泳帽", Description: "硅胶游泳帽"},
	}
	if err := database.Create(&categories).Error; err != nil {
		return err
	}

	// ---------- 器材（各场馆配置） ----------
	equipments := []models.Equipment{
		// 篮球馆
		{CategoryID: categories[1].ID, VenueID: venues[0].ID, Name: "斯伯丁7号篮球", UnitPrice: 30, Deposit: 200, TotalStock: 12, WarningStock: 3, Status: "active"},
		{CategoryID: categories[3].ID, VenueID: venues[0].ID, Name: "专业运动护腕", UnitPrice: 5, Deposit: 30, TotalStock: 30, WarningStock: 5, Status: "active"},
		{CategoryID: categories[4].ID, VenueID: venues[0].ID, Name: "专业运动护膝", UnitPrice: 8, Deposit: 50, TotalStock: 20, WarningStock: 3, Status: "active"},
		// 游泳馆
		{CategoryID: categories[5].ID, VenueID: venues[1].ID, Name: "速比涛防雾泳镜", UnitPrice: 10, Deposit: 80, TotalStock: 40, WarningStock: 8, Status: "active"},
		{CategoryID: categories[6].ID, VenueID: venues[1].ID, Name: "硅胶泳帽", UnitPrice: 5, Deposit: 20, TotalStock: 60, WarningStock: 10, Status: "active"},
		// 羽毛球馆（当前维护中，仍有库存）
		{CategoryID: categories[0].ID, VenueID: venues[2].ID, Name: "尤尼克斯羽毛球拍", UnitPrice: 20, Deposit: 150, TotalStock: 20, WarningStock: 4, Status: "active"},
		// 足球公园
		{CategoryID: categories[2].ID, VenueID: venues[3].ID, Name: "阿迪达斯5号足球", UnitPrice: 40, Deposit: 300, TotalStock: 8, WarningStock: 2, Status: "active"},
		{CategoryID: categories[3].ID, VenueID: venues[3].ID, Name: "专业运动护腕", UnitPrice: 5, Deposit: 30, TotalStock: 25, WarningStock: 5, Status: "active"},
		{CategoryID: categories[4].ID, VenueID: venues[3].ID, Name: "专业运动护膝", UnitPrice: 8, Deposit: 50, TotalStock: 15, WarningStock: 3, Status: "active"},
	}
	if err := database.Create(&equipments).Error; err != nil {
		return err
	}

	// ---------- 单件器材（按总库存生成编号） ----------
	var items []models.EquipmentItem
	purchaseDate := time.Date(2025, 12, 1, 0, 0, 0, 0, time.Local)
	for _, eq := range equipments {
		for i := 1; i <= eq.TotalStock; i++ {
			items = append(items, models.EquipmentItem{
				EquipmentID:   eq.ID,
				SerialNo:      fmt.Sprintf("EQ%04d-%03d", eq.ID, i),
				Status:        "in_stock",
				Location:      fmt.Sprintf("%s-A区-%d号架", venues[eq.VenueID-1].Name, (i-1)/5+1),
				PurchasePrice: eq.Deposit * 0.8,
				PurchaseDate:  &purchaseDate,
			})
		}
	}
	if err := database.Create(&items).Error; err != nil {
		return err
	}

	// ---------- 为其中一个预订附带器材租赁（bookings[0] 篮球馆 18-20点） ----------
	// 注意：种子数据里我们模拟的是"已经领用完成"的场景，所以 rental 状态 = picked，并把单件置 rented
	rentalHours := bookings[0].EndHour - bookings[0].StartHour
	pickupTime := time.Date(2026, 6, 20, 18, 0, 0, 0, time.Local)
	rental := models.EquipmentRental{
		BookingID:    bookings[0].ID,
		VenueID:      venues[0].ID,
		TotalDeposit: float64(2)*equipments[0].Deposit + float64(4)*equipments[2].Deposit,
		TotalRentFee: float64(2)*equipments[0].UnitPrice*float64(rentalHours) + float64(4)*equipments[2].UnitPrice*float64(rentalHours),
		Status:       "picked",
		PickedAt:     &pickupTime,
		Remark:       "预订同时租赁器材（种子模拟已领用场景）",
	}
	if err := database.Create(&rental).Error; err != nil {
		return err
	}

	// 选2个篮球单件
	var basketballItems []models.EquipmentItem
	database.Where("equipment_id = ? AND status = ?", equipments[0].ID, "in_stock").Limit(2).Find(&basketballItems)
	// 选4个护膝单件
	var kneeItems []models.EquipmentItem
	database.Where("equipment_id = ? AND status = ?", equipments[2].ID, "in_stock").Limit(4).Find(&kneeItems)

	rentalItems := []models.EquipmentRentalItem{}
	for _, bi := range basketballItems {
		rentalItems = append(rentalItems, models.EquipmentRentalItem{
			RentalID:        rental.ID,
			EquipmentID:     equipments[0].ID,
			EquipmentItemID: bi.ID,
			Quantity:        1,
			UnitPrice:       equipments[0].UnitPrice,
			Deposit:         equipments[0].Deposit,
			SubDeposit:      equipments[0].Deposit,
			SubRentFee:      equipments[0].UnitPrice * float64(rentalHours),
		})
	}
	for _, ki := range kneeItems {
		rentalItems = append(rentalItems, models.EquipmentRentalItem{
			RentalID:        rental.ID,
			EquipmentID:     equipments[2].ID,
			EquipmentItemID: ki.ID,
			Quantity:        1,
			UnitPrice:       equipments[2].UnitPrice,
			Deposit:         equipments[2].Deposit,
			SubDeposit:      equipments[2].Deposit,
			SubRentFee:      equipments[2].UnitPrice * float64(rentalHours),
		})
	}
	if err := database.Create(&rentalItems).Error; err != nil {
		return err
	}

	// 将分配的单件标记为 rented
	for _, bi := range basketballItems {
		database.Model(&models.EquipmentItem{}).Where("id = ?", bi.ID).Update("status", "rented")
	}
	for _, ki := range kneeItems {
		database.Model(&models.EquipmentItem{}).Where("id = ?", ki.ID).Update("status", "rented")
	}

	// 写入库存占用锁定
	locks := []models.EquipmentStockLock{
		{EquipmentID: equipments[0].ID, VenueID: venues[0].ID, BookingID: bookings[0].ID, RentalID: rental.ID, BookDate: bookings[0].BookDate, StartHour: bookings[0].StartHour, EndHour: bookings[0].EndHour, Quantity: 2},
		{EquipmentID: equipments[2].ID, VenueID: venues[0].ID, BookingID: bookings[0].ID, RentalID: rental.ID, BookDate: bookings[0].BookDate, StartHour: bookings[0].StartHour, EndHour: bookings[0].EndHour, Quantity: 4},
	}
	if err := database.Create(&locks).Error; err != nil {
		return err
	}

	// 更新预订的器材押金
	database.Model(&bookings[0]).Update("equipment_deposit", rental.TotalDeposit)

	log.Println("种子数据（含器材租赁）初始化完成")
	return nil
}
