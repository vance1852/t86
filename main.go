package main

import (
	"log"

	"github.com/gin-gonic/gin"

	"venue-booking-admin/internal/auth"
	"venue-booking-admin/internal/config"
	"venue-booking-admin/internal/db"
	"venue-booking-admin/internal/handlers"
	"venue-booking-admin/internal/seed"
)

func main() {
	cfg := config.Load()
	auth.SetSecret(cfg.JWTSecret)

	database, err := db.Connect(cfg.DSN)
	if err != nil {
		log.Fatalf("无法连接数据库: %v", err)
	}
	if err := db.Migrate(database); err != nil {
		log.Fatalf("数据库迁移失败: %v", err)
	}
	if err := seed.Run(database, cfg.AdminUsername, cfg.AdminPassword); err != nil {
		log.Fatalf("种子数据初始化失败: %v", err)
	}

	h := &handlers.Handler{DB: database}

	gin.SetMode(gin.ReleaseMode)
	r := gin.Default()

	api := r.Group("/api")
	{
		api.GET("/health", h.Health)
		api.POST("/auth/login", h.Login)

		secured := api.Group("")
		secured.Use(auth.Middleware(database))
		{
			secured.GET("/auth/me", h.Me)

			secured.GET("/venues", h.ListVenues)
			secured.POST("/venues", h.CreateVenue)
			secured.GET("/venues/:id", h.GetVenue)
			secured.PUT("/venues/:id", h.UpdateVenue)
			secured.DELETE("/venues/:id", h.DeleteVenue)

			secured.GET("/bookings", h.ListBookings)
			secured.POST("/bookings", h.CreateBooking)
			secured.PATCH("/bookings/:id/status", h.UpdateBookingStatus)
			secured.POST("/bookings/:id/settle-deposit", h.SettleDeposit)

			secured.GET("/dashboard/stats", h.DashboardStats)

			// ---------- 器材类别 ----------
			secured.GET("/equipment-categories", h.ListEquipmentCategories)
			secured.POST("/equipment-categories", h.CreateEquipmentCategory)
			secured.PUT("/equipment-categories/:id", h.UpdateEquipmentCategory)
			secured.DELETE("/equipment-categories/:id", h.DeleteEquipmentCategory)

			// ---------- 器材（聚合） ----------
			secured.GET("/equipments", h.ListEquipments)
			secured.GET("/equipments/:id", h.GetEquipment)
			secured.POST("/equipments", h.CreateEquipment)
			secured.PUT("/equipments/:id", h.UpdateEquipment)
			secured.DELETE("/equipments/:id", h.DeleteEquipment)

			// ---------- 单件器材 ----------
			secured.GET("/equipment-items", h.ListEquipmentItems)
			secured.POST("/equipment-items", h.CreateEquipmentItem)
			secured.PUT("/equipment-items/:id", h.UpdateEquipmentItem)
			secured.DELETE("/equipment-items/:id", h.DeleteEquipmentItem)
			secured.POST("/equipment-items/scrap", h.ScrapEquipmentItems)

			// ---------- 库存查询 / 预警 / 日志 ----------
			secured.GET("/stock/availability", h.QueryStockAvailability)
			secured.GET("/stock/low-warning", h.LowStockWarning)
			secured.GET("/inventory-logs", h.ListInventoryLogs)

			// ---------- 跨场馆调拨 ----------
			secured.GET("/transfers", h.ListTransfers)
			secured.POST("/transfers", h.CreateTransfer)
			secured.POST("/transfers/:id/complete", h.CompleteTransfer)
			secured.POST("/transfers/:id/cancel", h.CancelTransfer)

			// ---------- 盘点 ----------
			secured.GET("/stock-checks", h.ListStockChecks)
			secured.POST("/stock-checks", h.CreateStockCheck)
			secured.POST("/stock-checks/:id/confirm", h.ConfirmStockCheck)

			// ---------- 采购入库 ----------
			secured.GET("/purchases", h.ListPurchases)
			secured.POST("/purchases", h.CreatePurchase)

			// ---------- 租赁 ----------
			secured.GET("/rentals", h.ListRentals)
			secured.GET("/rentals/:id", h.GetRental)
			secured.POST("/rentals/:id/pickup", h.PickupRental)
			secured.POST("/rentals/:id/return", h.ReturnRental)
			secured.POST("/rentals/:id/cancel", h.CancelRental)

			// ---------- 赔付记录 ----------
			secured.GET("/compensations", h.ListCompensations)

			// ---------- 统计 ----------
			secured.GET("/stats/rentals", h.RentalStats)
			secured.GET("/stats/equipment-items", h.EquipmentItemUsageStats)
		}
	}

	log.Printf("venue-booking-admin listening on :%s", cfg.Port)
	if err := r.Run("0.0.0.0:" + cfg.Port); err != nil {
		log.Fatal(err)
	}
}
