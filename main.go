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

			secured.GET("/dashboard/stats", h.DashboardStats)
		}
	}

	log.Printf("venue-booking-admin listening on :%s", cfg.Port)
	if err := r.Run("0.0.0.0:" + cfg.Port); err != nil {
		log.Fatal(err)
	}
}
