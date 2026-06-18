package handlers

import (
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	"venue-booking-admin/internal/auth"
	"venue-booking-admin/internal/models"
)

// Handler 持有数据库句柄。
type Handler struct {
	DB *gorm.DB
}

// ---------- 认证 ----------

type loginReq struct {
	Username string `json:"username" binding:"required"`
	Password string `json:"password" binding:"required"`
}

func (h *Handler) Login(c *gin.Context) {
	var req loginReq
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusUnprocessableEntity, gin.H{"detail": "请求参数不合法"})
		return
	}
	var user models.User
	if err := h.DB.Where("username = ?", req.Username).First(&user).Error; err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"detail": "用户名或密码错误"})
		return
	}
	if !auth.VerifyPassword(req.Password, user.PasswordHash) {
		c.JSON(http.StatusUnauthorized, gin.H{"detail": "用户名或密码错误"})
		return
	}
	token, err := auth.CreateToken(user.ID, user.Username)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"detail": "签发令牌失败"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"access_token": token, "token_type": "bearer"})
}

func (h *Handler) Me(c *gin.Context) {
	user := c.MustGet("user").(models.User)
	c.JSON(http.StatusOK, gin.H{"id": user.ID, "username": user.Username, "display_name": user.DisplayName})
}

// ---------- 场馆 ----------

type venueReq struct {
	Name        string  `json:"name" binding:"required"`
	SportType   string  `json:"sport_type"`
	Capacity    int     `json:"capacity"`
	HourlyPrice float64 `json:"hourly_price"`
	OpenHour    int     `json:"open_hour"`
	CloseHour   int     `json:"close_hour"`
	Status      string  `json:"status"`
}

func (h *Handler) ListVenues(c *gin.Context) {
	var venues []models.Venue
	h.DB.Order("id").Find(&venues)
	c.JSON(http.StatusOK, venues)
}

func (h *Handler) CreateVenue(c *gin.Context) {
	var req venueReq
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusUnprocessableEntity, gin.H{"detail": "请求参数不合法"})
		return
	}
	if req.CloseHour <= req.OpenHour {
		c.JSON(http.StatusUnprocessableEntity, gin.H{"detail": "关闭时间须晚于开放时间"})
		return
	}
	status := req.Status
	if status == "" {
		status = "open"
	}
	venue := models.Venue{
		Name: req.Name, SportType: req.SportType, Capacity: req.Capacity,
		HourlyPrice: req.HourlyPrice, OpenHour: req.OpenHour, CloseHour: req.CloseHour, Status: status,
	}
	h.DB.Create(&venue)
	c.JSON(http.StatusCreated, venue)
}

func (h *Handler) GetVenue(c *gin.Context) {
	var venue models.Venue
	if err := h.DB.First(&venue, c.Param("id")).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"detail": "场馆不存在"})
		return
	}
	c.JSON(http.StatusOK, venue)
}

func (h *Handler) UpdateVenue(c *gin.Context) {
	var venue models.Venue
	if err := h.DB.First(&venue, c.Param("id")).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"detail": "场馆不存在"})
		return
	}
	var req venueReq
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusUnprocessableEntity, gin.H{"detail": "请求参数不合法"})
		return
	}
	venue.Name = req.Name
	venue.SportType = req.SportType
	venue.Capacity = req.Capacity
	venue.HourlyPrice = req.HourlyPrice
	if req.OpenHour != 0 || req.CloseHour != 0 {
		venue.OpenHour = req.OpenHour
		venue.CloseHour = req.CloseHour
	}
	if req.Status != "" {
		venue.Status = req.Status
	}
	h.DB.Save(&venue)
	c.JSON(http.StatusOK, venue)
}

func (h *Handler) DeleteVenue(c *gin.Context) {
	var venue models.Venue
	if err := h.DB.First(&venue, c.Param("id")).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"detail": "场馆不存在"})
		return
	}
	h.DB.Delete(&venue)
	c.Status(http.StatusNoContent)
}

// ---------- 预订 ----------

type bookingReq struct {
	VenueID      uint                 `json:"venue_id" binding:"required"`
	CustomerName string               `json:"customer_name" binding:"required"`
	Phone        string               `json:"phone"`
	BookDate     string               `json:"book_date" binding:"required"`
	StartHour    int                  `json:"start_hour"`
	EndHour      int                  `json:"end_hour"`
	VenueDeposit float64              `json:"venue_deposit"`
	Equipments   []bookingEquipmentItem `json:"equipments"`
}

func (h *Handler) ListBookings(c *gin.Context) {
	var bookings []models.Booking
	q := h.DB.Order("id desc")
	if vid := c.Query("venue_id"); vid != "" {
		q = q.Where("venue_id = ?", vid)
	}
	if d := c.Query("date"); d != "" {
		q = q.Where("book_date = ?", d)
	}
	q.Find(&bookings)
	c.JSON(http.StatusOK, bookings)
}

func (h *Handler) CreateBooking(c *gin.Context) {
	var req bookingReq
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusUnprocessableEntity, gin.H{"detail": "请求参数不合法"})
		return
	}
	if req.EndHour <= req.StartHour {
		c.JSON(http.StatusUnprocessableEntity, gin.H{"detail": "结束时间须晚于开始时间"})
		return
	}
	var venue models.Venue
	if err := h.DB.First(&venue, req.VenueID).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"detail": "场馆不存在"})
		return
	}
	if venue.Status != "open" {
		c.JSON(http.StatusUnprocessableEntity, gin.H{"detail": "该场馆当前不可预订"})
		return
	}
	if req.StartHour < venue.OpenHour || req.EndHour > venue.CloseHour {
		c.JSON(http.StatusUnprocessableEntity, gin.H{"detail": "预订时段超出场馆开放时间"})
		return
	}

	operator := getOperator(c)
	var booking models.Booking
	var rental *models.EquipmentRental

	err := h.DB.Transaction(func(tx *gorm.DB) error {
		// 时段冲突校验：同场馆同日，已有非取消预订时段不得与本次重叠
		var conflict int64
		tx.Model(&models.Booking{}).
			Where("venue_id = ? AND book_date = ? AND status <> ?", req.VenueID, req.BookDate, "cancelled").
			Where("start_hour < ? AND end_hour > ?", req.EndHour, req.StartHour).
			Count(&conflict)
		if conflict > 0 {
			return errors.New("venue time slot conflict")
		}

		amount := venue.HourlyPrice * float64(req.EndHour-req.StartHour)
		booking = models.Booking{
			VenueID:      req.VenueID,
			CustomerName: req.CustomerName,
			Phone:        req.Phone,
			BookDate:     req.BookDate,
			StartHour:    req.StartHour,
			EndHour:      req.EndHour,
			Amount:       amount,
			VenueDeposit: req.VenueDeposit,
			Status:       "booked",
		}
		if err := tx.Create(&booking).Error; err != nil {
			return err
		}

		// 创建租赁单（事务内，含库存占用校验、单件分配、库存锁定）
		r, err := h.createRentalForBooking(tx, &booking, req.Equipments, operator)
		if err != nil {
			return err
		}
		rental = r
		return nil
	})
	if err != nil {
		c.JSON(http.StatusConflict, gin.H{"detail": "创建失败：场馆时段冲突或器材库存不足"})
		return
	}

	resp := gin.H{"booking": booking}
	if rental != nil {
		resp["rental"] = rental
	}
	c.JSON(http.StatusCreated, resp)
}

type statusReq struct {
	Status string `json:"status" binding:"required"`
}

func (h *Handler) UpdateBookingStatus(c *gin.Context) {
	var booking models.Booking
	if err := h.DB.First(&booking, c.Param("id")).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"detail": "预订不存在"})
		return
	}
	var req statusReq
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusUnprocessableEntity, gin.H{"detail": "状态不合法"})
		return
	}
	if req.Status != "booked" && req.Status != "cancelled" && req.Status != "completed" {
		c.JSON(http.StatusUnprocessableEntity, gin.H{"detail": "状态不合法"})
		return
	}
	booking.Status = req.Status
	h.DB.Save(&booking)
	c.JSON(http.StatusOK, gin.H{"id": booking.ID, "status": booking.Status})
}

// ---------- 仪表盘 ----------

func (h *Handler) DashboardStats(c *gin.Context) {
	var venueTotal, venueOpen, bookingTotal, bookingActive int64
	h.DB.Model(&models.Venue{}).Count(&venueTotal)
	h.DB.Model(&models.Venue{}).Where("status = ?", "open").Count(&venueOpen)
	h.DB.Model(&models.Booking{}).Count(&bookingTotal)
	h.DB.Model(&models.Booking{}).Where("status = ?", "booked").Count(&bookingActive)

	var revenue float64
	h.DB.Model(&models.Booking{}).Where("status <> ?", "cancelled").
		Select("COALESCE(SUM(amount),0)").Scan(&revenue)

	// 器材相关统计
	var eqTotal, eqInStock, eqRented, eqRepairing, eqScrapped int64
	h.DB.Model(&models.Equipment{}).Count(&eqTotal)
	h.DB.Model(&models.EquipmentItem{}).Where("status = ?", "in_stock").Count(&eqInStock)
	h.DB.Model(&models.EquipmentItem{}).Where("status = ?", "rented").Count(&eqRented)
	h.DB.Model(&models.EquipmentItem{}).Where("status = ?", "repairing").Count(&eqRepairing)
	h.DB.Model(&models.EquipmentItem{}).Where("status = ?", "scrapped").Count(&eqScrapped)

	var rentalTotal, rentalActive int64
	h.DB.Model(&models.EquipmentRental{}).Count(&rentalTotal)
	h.DB.Model(&models.EquipmentRental{}).Where("status IN ?", []string{"frozen", "picked"}).Count(&rentalActive)

	var rentRevenue, totalCompensation float64
	h.DB.Model(&models.EquipmentRental{}).Select("COALESCE(SUM(total_rent_fee),0)").Scan(&rentRevenue)
	h.DB.Model(&models.EquipmentCompensation{}).Select("COALESCE(SUM(amount),0)").Scan(&totalCompensation)

	// 低库存预警数
	var lowStockCount int64
	var equipments []models.Equipment
	h.DB.Find(&equipments)
	for _, eq := range equipments {
		var inStock int64
		h.DB.Model(&models.EquipmentItem{}).Where("equipment_id = ? AND status = ?", eq.ID, "in_stock").Count(&inStock)
		if int(inStock) <= eq.WarningStock {
			lowStockCount++
		}
	}

	c.JSON(http.StatusOK, gin.H{
		"venue_total":     venueTotal,
		"venue_open":      venueOpen,
		"booking_total":   bookingTotal,
		"booking_active":  bookingActive,
		"revenue_total":   revenue,
		"equipment_total":   eqTotal,
		"equipment_in_stock": eqInStock,
		"equipment_rented":   eqRented,
		"equipment_repairing": eqRepairing,
		"equipment_scrapped":  eqScrapped,
		"rental_total":       rentalTotal,
		"rental_active":      rentalActive,
		"rental_revenue":     rentRevenue,
		"compensation_total": totalCompensation,
		"low_stock_count":    lowStockCount,
	})
}

// Health 健康检查。
func (h *Handler) Health(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{"status": "ok", "service": "venue-booking-admin"})
}
