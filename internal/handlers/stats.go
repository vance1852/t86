package handlers

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"venue-booking-admin/internal/models"
)

// ---------- 租赁与损耗统计 ----------

type rentalStatsReq struct {
	VenueID  uint   `form:"venue_id"`
	StartDate string `form:"start_date"`
	EndDate   string `form:"end_date"`
}

func (h *Handler) RentalStats(c *gin.Context) {
	var req rentalStatsReq
	if err := c.ShouldBindQuery(&req); err != nil {
		c.JSON(http.StatusUnprocessableEntity, gin.H{"detail": "请求参数不合法"})
		return
	}

	type rentalAgg struct {
		TotalRentals      int64
		TotalRentFee      float64
		TotalDeposit      float64
		TotalCompensation float64
		TotalRefund       float64
		Returned          int64
		Damaged           int64
		Lost              int64
	}

	var agg rentalAgg
	q := h.DB.Model(&models.EquipmentRental{})
	if req.VenueID > 0 {
		q = q.Where("venue_id = ?", req.VenueID)
	}
	if req.StartDate != "" {
		q = q.Where("DATE(created_at) >= ?", req.StartDate)
	}
	if req.EndDate != "" {
		q = q.Where("DATE(created_at) <= ?", req.EndDate)
	}
	q.Select(`
		COUNT(*) as total_rentals,
		COALESCE(SUM(total_rent_fee),0) as total_rent_fee,
		COALESCE(SUM(total_deposit),0) as total_deposit,
		COALESCE(SUM(compensation),0) as total_compensation,
		COALESCE(SUM(refund_deposit),0) as total_refund
	`).Scan(&agg)

	q2 := h.DB.Model(&models.EquipmentRental{})
	if req.VenueID > 0 {
		q2 = q2.Where("venue_id = ?", req.VenueID)
	}
	if req.StartDate != "" {
		q2 = q2.Where("DATE(created_at) >= ?", req.StartDate)
	}
	if req.EndDate != "" {
		q2 = q2.Where("DATE(created_at) <= ?", req.EndDate)
	}
	q2.Where("status = ?", "returned").Count(&agg.Returned)
	q3 := h.DB.Model(&models.EquipmentRental{})
	if req.VenueID > 0 {
		q3 = q3.Where("venue_id = ?", req.VenueID)
	}
	if req.StartDate != "" {
		q3 = q3.Where("DATE(created_at) >= ?", req.StartDate)
	}
	if req.EndDate != "" {
		q3 = q3.Where("DATE(created_at) <= ?", req.EndDate)
	}
	q3.Where("status = ?", "damaged").Count(&agg.Damaged)

	var lostCount int64
	q4 := h.DB.Model(&models.EquipmentRentalItem{}).
		Joins("JOIN equipment_rentals ON equipment_rentals.id = equipment_rental_items.rental_id")
	if req.VenueID > 0 {
		q4 = q4.Where("equipment_rentals.venue_id = ?", req.VenueID)
	}
	if req.StartDate != "" {
		q4 = q4.Where("DATE(equipment_rentals.created_at) >= ?", req.StartDate)
	}
	if req.EndDate != "" {
		q4 = q4.Where("DATE(equipment_rentals.created_at) <= ?", req.EndDate)
	}
	q4.Where("equipment_rental_items.return_status IN ?", []string{"lost", "not_returned"}).Count(&lostCount)
	agg.Lost = lostCount

	// 器材使用率排行 TOP 10
	type usageRank struct {
		EquipmentID   uint    `json:"equipment_id"`
		EquipmentName string  `json:"equipment_name"`
		RentCount     int64   `json:"rent_count"`
		Revenue       float64 `json:"revenue"`
	}
	var ranks []usageRank
	q5 := h.DB.Model(&models.EquipmentRentalItem{}).
		Select(`equipment_rental_items.equipment_id,
			MAX(equipments.name) as equipment_name,
			COUNT(*) as rent_count,
			COALESCE(SUM(equipment_rental_items.sub_rent_fee),0) as revenue`).
		Joins("JOIN equipment_rentals ON equipment_rentals.id = equipment_rental_items.rental_id").
		Joins("JOIN equipments ON equipments.id = equipment_rental_items.equipment_id")
	if req.VenueID > 0 {
		q5 = q5.Where("equipment_rentals.venue_id = ?", req.VenueID)
	}
	if req.StartDate != "" {
		q5 = q5.Where("DATE(equipment_rentals.created_at) >= ?", req.StartDate)
	}
	if req.EndDate != "" {
		q5 = q5.Where("DATE(equipment_rentals.created_at) <= ?", req.EndDate)
	}
	q5.Group("equipment_rental_items.equipment_id").
		Order("rent_count desc").
		Limit(10).
		Scan(&ranks)

	// 损耗排行 TOP 10（按赔付金额）
	type lossRank struct {
		EquipmentID   uint    `json:"equipment_id"`
		EquipmentName string  `json:"equipment_name"`
		LossCount     int64   `json:"loss_count"`
		LossAmount    float64 `json:"loss_amount"`
	}
	var losses []lossRank
	q6 := h.DB.Model(&models.EquipmentCompensation{}).
		Select(`equipment_compensations.equipment_item_id as equipment_id,  -- placeholder
			MAX(equipments.name) as equipment_name,
			COUNT(*) as loss_count,
			COALESCE(SUM(equipment_compensations.amount),0) as loss_amount`).
		Joins("JOIN equipment_rental_items ON equipment_rental_items.id = equipment_compensations.rental_item_id").
		Joins("JOIN equipments ON equipments.id = equipment_rental_items.equipment_id").
		Joins("JOIN equipment_rentals ON equipment_rentals.id = equipment_rental_items.rental_id")
	if req.VenueID > 0 {
		q6 = q6.Where("equipment_rentals.venue_id = ?", req.VenueID)
	}
	if req.StartDate != "" {
		q6 = q6.Where("DATE(equipment_compensations.created_at) >= ?", req.StartDate)
	}
	if req.EndDate != "" {
		q6 = q6.Where("DATE(equipment_compensations.created_at) <= ?", req.EndDate)
	}
	q6.Group("equipment_rental_items.equipment_id").
		Order("loss_amount desc").
		Limit(10).
		Scan(&losses)

	c.JSON(http.StatusOK, gin.H{
		"summary":       agg,
		"usage_ranking":  ranks,
		"loss_ranking":   losses,
	})
}

// ---------- 单件器材使用率明细 ----------

func (h *Handler) EquipmentItemUsageStats(c *gin.Context) {
	var list []models.EquipmentItem
	q := h.DB.Preload("Equipment").Order("id desc")
	if eid := c.Query("equipment_id"); eid != "" {
		q = q.Where("equipment_id = ?", eid)
	}
	if vid := c.Query("venue_id"); vid != "" {
		q = q.Joins("JOIN equipments ON equipments.id = equipment_items.equipment_id").Where("equipments.venue_id = ?", vid)
	}
	q.Limit(500).Find(&list)

	result := make([]gin.H, 0, len(list))
	for _, item := range list {
		var rentCount int64
		h.DB.Model(&models.EquipmentRentalItem{}).Where("equipment_item_id = ?", item.ID).Count(&rentCount)
		var totalRentFee float64
		h.DB.Model(&models.EquipmentRentalItem{}).Where("equipment_item_id = ?", item.ID).Select("COALESCE(SUM(sub_rent_fee),0)").Scan(&totalRentFee)
		var totalCompensation float64
		h.DB.Model(&models.EquipmentCompensation{}).Where("equipment_item_id = ?", item.ID).Select("COALESCE(SUM(amount),0)").Scan(&totalCompensation)

		result = append(result, gin.H{
			"item":               item,
			"rent_count":         rentCount,
			"total_rent_fee":     totalRentFee,
			"total_compensation": totalCompensation,
		})
	}
	c.JSON(http.StatusOK, result)
}
