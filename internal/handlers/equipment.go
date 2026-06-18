package handlers

import (
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	"venue-booking-admin/internal/models"
)

// ---------- 工具函数 ----------

func getOperator(c *gin.Context) string {
	if u, ok := c.Get("user"); ok {
		if user, ok := u.(models.User); ok {
			return user.Username
		}
	}
	return "system"
}

// addInventoryLog 写入库存变动日志。
func (h *Handler) addInventoryLog(tx *gorm.DB, equipmentID, venueID, relatedID uint, changeType string, qty, beforeStock int, remark, operator string) error {
	return tx.Create(&models.InventoryLog{
		EquipmentID: equipmentID,
		VenueID:   venueID,
		ChangeType: changeType,
		Quantity:   qty,
		BeforeStock: beforeStock,
		AfterStock:  beforeStock + qty,
		RelatedID:  relatedID,
		Remark:     remark,
		Operator:   operator,
	}).Error
}

// calcAvailableStock 计算某个器材在指定时段的可用数量（总在库 - 同时段已锁定占用）。
// 用于并发下的库存占用计算。
func (h *Handler) calcAvailableStock(tx *gorm.DB, equipmentID uint, venueID uint, bookDate string, startHour, endHour int) (int, error) {
	var eq models.Equipment
	if err := tx.Where("id = ? AND venue_id = ?", equipmentID, venueID).First(&eq).Error; err != nil {
		return 0, err
	}
	// 在库且状态
	var inStock int64
	tx.Model(&models.EquipmentItem{}).Where("equipment_id = ? AND status = ?", equipmentID, "in_stock").Count(&inStock)

	// 时段重叠的锁定占用量
	var locked int64
	tx.Model(&models.EquipmentStockLock{}).
		Where("equipment_id = ? AND venue_id = ? AND book_date = ?", equipmentID, venueID, bookDate).
		Where("start_hour < ? AND end_hour > ?", endHour, startHour).
		Select("COALESCE(SUM(quantity),0)").Scan(&locked)

	available := int(inStock) - int(locked)
	if available < 0 {
		available = 0
	}
	return available, nil
}

// ---------- 器材类别 ----------

type categoryReq struct {
	Name        string `json:"name" binding:"required"`
	Description string `json:"description"`
}

func (h *Handler) ListEquipmentCategories(c *gin.Context) {
	var list []models.EquipmentCategory
	h.DB.Order("id").Find(&list)
	c.JSON(http.StatusOK, list)
}

func (h *Handler) CreateEquipmentCategory(c *gin.Context) {
	var req categoryReq
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusUnprocessableEntity, gin.H{"detail": "请求参数不合法"})
		return
	}
	cat := models.EquipmentCategory{Name: req.Name, Description: req.Description}
	if err := h.DB.Create(&cat).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"detail": "创建失败，类别名可能重复"})
		return
	}
	c.JSON(http.StatusCreated, cat)
}

func (h *Handler) UpdateEquipmentCategory(c *gin.Context) {
	var cat models.EquipmentCategory
	if err := h.DB.First(&cat, c.Param("id")).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"detail": "类别不存在"})
		return
	}
	var req categoryReq
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusUnprocessableEntity, gin.H{"detail": "请求参数不合法"})
		return
	}
	cat.Name = req.Name
	cat.Description = req.Description
	h.DB.Save(&cat)
	c.JSON(http.StatusOK, cat)
}

func (h *Handler) DeleteEquipmentCategory(c *gin.Context) {
	var cat models.EquipmentCategory
	if err := h.DB.First(&cat, c.Param("id")).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"detail": "类别不存在"})
		return
	}
	h.DB.Delete(&cat)
	c.Status(http.StatusNoContent)
}

// ---------- 器材（聚合） ----------

type equipmentReq struct {
	CategoryID   uint    `json:"category_id" binding:"required"`
	VenueID      uint    `json:"venue_id" binding:"required"`
	Name         string  `json:"name" binding:"required"`
	UnitPrice    float64 `json:"unit_price"`
	Deposit      float64 `json:"deposit"`
	WarningStock int     `json:"warning_stock"`
	Status       string  `json:"status"`
}

func (h *Handler) ListEquipments(c *gin.Context) {
	var list []models.Equipment
	q := h.DB.Preload("Category").Preload("Venue").Order("id")
	if vid := c.Query("venue_id"); vid != "" {
		q = q.Where("venue_id = ?", vid)
	}
	if cid := c.Query("category_id"); cid != "" {
		q = q.Where("category_id = ?", cid)
	}
	q.Find(&list)

	itemWithStock := make([]gin.H, 0, len(list))
	for _, eq := range list {
		var inStock, rented, repairing, scrapped int64
		h.DB.Model(&models.EquipmentItem{}).Where("equipment_id = ? AND status = ?", eq.ID, "in_stock").Count(&inStock)
		h.DB.Model(&models.EquipmentItem{}).Where("equipment_id = ? AND status = ?", eq.ID, "rented").Count(&rented)
		h.DB.Model(&models.EquipmentItem{}).Where("equipment_id = ? AND status = ?", eq.ID, "repairing").Count(&repairing)
		h.DB.Model(&models.EquipmentItem{}).Where("equipment_id = ? AND status = ?", eq.ID, "scrapped").Count(&scrapped)
		itemWithStock = append(itemWithStock, gin.H{
			"id":             eq.ID,
			"category_id":      eq.CategoryID,
			"venue_id":       eq.VenueID,
			"name":           eq.Name,
			"unit_price":     eq.UnitPrice,
			"deposit":        eq.Deposit,
			"total_stock":    eq.TotalStock,
			"warning_stock":  eq.WarningStock,
			"status":         eq.Status,
			"category":       eq.Category,
			"venue":          eq.Venue,
			"created_at":     eq.CreatedAt,
			"stock_in_stock":  inStock,
			"stock_rented":    rented,
			"stock_repairing": repairing,
			"stock_scrapped":  scrapped,
		})
	}
	c.JSON(http.StatusOK, itemWithStock)
}

func (h *Handler) GetEquipment(c *gin.Context) {
	var eq models.Equipment
	if err := h.DB.Preload("Category").Preload("Venue").First(&eq, c.Param("id")).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"detail": "器材不存在"})
		return
	}
	c.JSON(http.StatusOK, eq)
}

func (h *Handler) CreateEquipment(c *gin.Context) {
	var req equipmentReq
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusUnprocessableEntity, gin.H{"detail": "请求参数不合法"})
		return
	}
	status := req.Status
	if status == "" {
		status = "active"
	}
	eq := models.Equipment{
		CategoryID:   req.CategoryID,
		VenueID:      req.VenueID,
		Name:         req.Name,
		UnitPrice:    req.UnitPrice,
		Deposit:      req.Deposit,
		WarningStock: req.WarningStock,
		Status:       status,
	}
	h.DB.Create(&eq)
	c.JSON(http.StatusCreated, eq)
}

func (h *Handler) UpdateEquipment(c *gin.Context) {
	var eq models.Equipment
	if err := h.DB.First(&eq, c.Param("id")).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"detail": "器材不存在"})
		return
	}
	var req equipmentReq
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusUnprocessableEntity, gin.H{"detail": "请求参数不合法"})
		return
	}
	eq.CategoryID = req.CategoryID
	eq.VenueID = req.VenueID
	eq.Name = req.Name
	eq.UnitPrice = req.UnitPrice
	eq.Deposit = req.Deposit
	eq.WarningStock = req.WarningStock
	if req.Status != "" {
		eq.Status = req.Status
	}
	h.DB.Save(&eq)
	c.JSON(http.StatusOK, eq)
}

func (h *Handler) DeleteEquipment(c *gin.Context) {
	var eq models.Equipment
	if err := h.DB.First(&eq, c.Param("id")).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"detail": "器材不存在"})
		return
	}
	var rentCount int64
	h.DB.Model(&models.EquipmentItem{}).Where("equipment_id = ? AND status IN ?", eq.ID, []string{"rented", "repairing"}).Count(&rentCount)
	if rentCount > 0 {
		c.JSON(http.StatusUnprocessableEntity, gin.H{"detail": "存在租出或维修中，无法删除"})
		return
	}
	h.DB.Delete(&eq)
	c.Status(http.StatusNoContent)
}

// ---------- 单件器材 ----------

type equipmentItemReq struct {
	EquipmentID   uint    `json:"equipment_id" binding:"required"`
	SerialNo    string  `json:"serial_no" binding:"required"`
	Status      string  `json:"status"`
	Location    string  `json:"location"`
	Remark      string  `json:"remark"`
	PurchasePrice float64 `json:"purchase_price"`
}

func (h *Handler) ListEquipmentItems(c *gin.Context) {
	var list []models.EquipmentItem
	q := h.DB.Preload("Equipment").Preload("Equipment.Category").Order("id desc")
	if eid := c.Query("equipment_id"); eid != "" {
		q = q.Where("equipment_id = ?", eid)
	}
	if status := c.Query("status"); status != "" {
		q = q.Where("status = ?", status)
	}
	if vid := c.Query("venue_id"); vid != "" {
		q = q.Joins("JOIN equipments ON equipments.id = equipment_items.equipment_id").Where("equipments.venue_id = ?", vid)
	}
	q.Find(&list)
	c.JSON(http.StatusOK, list)
}

func (h *Handler) CreateEquipmentItem(c *gin.Context) {
	var req equipmentItemReq
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusUnprocessableEntity, gin.H{"detail": "请求参数不合法"})
		return
	}
	status := req.Status
	if status == "" {
		status = "in_stock"
	}
	var eq models.Equipment
	if err := h.DB.First(&eq, req.EquipmentID).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"detail": "所属器材不存在"})
		return
	}
	item := models.EquipmentItem{
		EquipmentID:   req.EquipmentID,
		SerialNo:    req.SerialNo,
		Status:      status,
		Location:    req.Location,
		Remark:      req.Remark,
		PurchasePrice: req.PurchasePrice,
	}
	if err := h.DB.Create(&item).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"detail": "创建失败，编号可能重复"})
		return
	}
	h.DB.Model(&eq).Update("total_stock", gorm.Expr("total_stock + 1"))
	c.JSON(http.StatusCreated, item)
}

func (h *Handler) UpdateEquipmentItem(c *gin.Context) {
	var item models.EquipmentItem
	if err := h.DB.First(&item, c.Param("id")).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"detail": "单件不存在"})
		return
	}
	var req equipmentItemReq
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusUnprocessableEntity, gin.H{"detail": "请求参数不合法"})
		return
	}
	item.SerialNo = req.SerialNo
	item.Location = req.Location
	item.Remark = req.Remark
	item.PurchasePrice = req.PurchasePrice
	if req.Status != "" {
		if req.Status == "scrapped" && item.Status != "scrapped" {
			now := time.Now()
			item.ScrappedAt = &now
		}
		item.Status = req.Status
	}
	h.DB.Save(&item)
	c.JSON(http.StatusOK, item)
}

func (h *Handler) DeleteEquipmentItem(c *gin.Context) {
	var item models.EquipmentItem
	if err := h.DB.First(&item, c.Param("id")).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"detail": "单件不存在"})
		return
	}
	if item.Status == "rented" {
		c.JSON(http.StatusUnprocessableEntity, gin.H{"detail": "已租出的单件不能删除"})
		return
	}
	h.DB.Delete(&item)
	h.DB.Model(&models.Equipment{}).Where("id = ?", item.EquipmentID).Update("total_stock", gorm.Expr("total_stock - 1"))
	c.Status(http.StatusNoContent)
}

// ---------- 库存查询（含时段可用性） ----------

type stockQueryReq struct {
	VenueID   uint   `form:"venue_id"`
	BookDate  string `form:"book_date"`
	StartHour  int    `form:"start_hour"`
	EndHour    int    `form:"end_hour"`
}

func (h *Handler) QueryStockAvailability(c *gin.Context) {
	var req stockQueryReq
	if err := c.ShouldBindQuery(&req); err != nil {
		c.JSON(http.StatusUnprocessableEntity, gin.H{"detail": "请求参数不合法"})
		return
	}
	var equipments []models.Equipment
	q := h.DB.Preload("Category").Preload("Venue")
	if req.VenueID > 0 {
		q = q.Where("venue_id = ?", req.VenueID)
	}
	q.Find(&equipments)

	result := make([]gin.H, 0, len(equipments))
	for _, eq := range equipments {
		var inStock int64
		h.DB.Model(&models.EquipmentItem{}).Where("equipment_id = ? AND status = ?", eq.ID, "in_stock").Count(&inStock)

		locked := int64(0)
		if req.BookDate != "" && req.EndHour > req.StartHour {
			h.DB.Model(&models.EquipmentStockLock{}).
				Where("equipment_id = ? AND book_date = ?", eq.ID, req.BookDate).
				Where("start_hour < ? AND end_hour > ?", req.EndHour, req.StartHour).
				Select("COALESCE(SUM(quantity),0)").Scan(&locked)
		}
		available := int(inStock) - int(locked)
		if available < 0 {
			available = 0
		}
		result = append(result, gin.H{
			"equipment":        eq,
			"in_stock":     inStock,
			"locked":         locked,
			"available":      available,
			"low_stock":      available <= eq.WarningStock,
			"warning_stock": eq.WarningStock,
		})
	}
	c.JSON(http.StatusOK, result)
}

// ---------- 低库存预警 ----------

func (h *Handler) LowStockWarning(c *gin.Context) {
	var equipments []models.Equipment
	h.DB.Preload("Category").Preload("Venue").Find(&equipments)

	warnings := make([]gin.H, 0)
	for _, eq := range equipments {
		var inStock int64
		h.DB.Model(&models.EquipmentItem{}).Where("equipment_id = ? AND status = ?", eq.ID, "in_stock").Count(&inStock)
		if int(inStock) <= eq.WarningStock {
			warnings = append(warnings, gin.H{
				"equipment":     eq,
				"in_stock":    inStock,
				"warning_stock": eq.WarningStock,
				"shortage":     eq.WarningStock - int(inStock),
			})
		}
	}
	c.JSON(http.StatusOK, warnings)
}

// ---------- 库存变动日志 ----------

func (h *Handler) ListInventoryLogs(c *gin.Context) {
	var logs []models.InventoryLog
	q := h.DB.Order("id desc")
	if eid := c.Query("equipment_id"); eid != "" {
		q = q.Where("equipment_id = ?", eid)
	}
	if vid := c.Query("venue_id"); vid != "" {
		q = q.Where("venue_id = ?", vid)
	}
	if ct := c.Query("change_type"); ct != "" {
		q = q.Where("change_type = ?", ct)
	}
	q.Limit(200).Find(&logs)
	c.JSON(http.StatusOK, logs)
}
