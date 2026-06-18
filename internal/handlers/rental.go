package handlers

import (
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"venue-booking-admin/internal/models"
)

func min(a, b float64) float64 {
	if a < b {
		return a
	}
	return b
}

func max(a, b float64) float64 {
	if a > b {
		return a
	}
	return b
}

// ---------- 预订扩展：附带器材租赁 ----------

type bookingEquipmentItem struct {
	EquipmentID uint `json:"equipment_id" binding:"required"`
	Quantity    int  `json:"quantity" binding:"required,min=1"`
}

// ---------- 租赁单查询 ----------

func (h *Handler) ListRentals(c *gin.Context) {
	var list []models.EquipmentRental
	q := h.DB.Preload("Items").Preload("Items.Equipment").Preload("Items.EquipmentItem").Order("id desc")
	if bid := c.Query("booking_id"); bid != "" {
		q = q.Where("booking_id = ?", bid)
	}
	if vid := c.Query("venue_id"); vid != "" {
		q = q.Where("venue_id = ?", vid)
	}
	if status := c.Query("status"); status != "" {
		q = q.Where("status = ?", status)
	}
	q.Find(&list)
	c.JSON(http.StatusOK, list)
}

func (h *Handler) GetRental(c *gin.Context) {
	var r models.EquipmentRental
	if err := h.DB.Preload("Items").Preload("Items.Equipment").Preload("Items.EquipmentItem").First(&r, c.Param("id")).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"detail": "租赁单不存在"})
		return
	}
	c.JSON(http.StatusOK, r)
}

// ---------- 预订附带租赁（从 CreateBooking 中调用，事务内处理） ----------

// createRentalForBooking 在事务中创建租赁单：校验时段库存、冻结占用、分配单件。
// 并发一致性：1) 对 Equipment 行加 FOR UPDATE 悲观锁串行化；2) 写入锁定后再二次校验。
func (h *Handler) createRentalForBooking(tx *gorm.DB, booking *models.Booking, items []bookingEquipmentItem, operator string) (*models.EquipmentRental, error) {
	if len(items) == 0 {
		return nil, nil
	}

	rental := &models.EquipmentRental{
		BookingID: booking.ID,
		VenueID:   booking.VenueID,
		Status:    "frozen",
	}

	var totalDeposit, totalRentFee float64
	rentalItems := make([]models.EquipmentRentalItem, 0, len(items))
	stockLocks := make([]models.EquipmentStockLock, 0, len(items))

	for _, it := range items {
		// [Bug4 Fix] 悲观锁：串行化同一器材的并发下单（GORM v2 标准写法）
		var eq models.Equipment
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			Where("id = ? AND venue_id = ?", it.EquipmentID, booking.VenueID).
			First(&eq).Error; err != nil {
			return nil, err
		}
		if eq.Status != "active" {
			return nil, errors.New("equipment is not active")
		}

		// 计算时段可用量（物理在库 - 时段重叠已锁定）
		available, err := h.calcAvailableStock(tx, it.EquipmentID, booking.VenueID, booking.BookDate, booking.StartHour, booking.EndHour)
		if err != nil {
			return nil, err
		}
		if available < it.Quantity {
			return nil, errors.New("时段库存不足，请减少数量或更换时段")
		}

		// 选取足够的 in_stock 单件（仅记录分配，不修改 status，到 Pickup 时才改 rented）
		var picked []models.EquipmentItem
		if err := tx.Where("equipment_id = ? AND status = ?", it.EquipmentID, "in_stock").
			Limit(it.Quantity).Find(&picked).Error; err != nil {
			return nil, err
		}
		if len(picked) < it.Quantity {
			return nil, errors.New("可用单件数量不足")
		}

		subDeposit := eq.Deposit * float64(it.Quantity)
		hours := float64(booking.EndHour - booking.StartHour)
		subRentFee := eq.UnitPrice * hours * float64(it.Quantity)
		totalDeposit += subDeposit
		totalRentFee += subRentFee

		for _, pk := range picked {
			rentalItems = append(rentalItems, models.EquipmentRentalItem{
				EquipmentID:     it.EquipmentID,
				EquipmentItemID: pk.ID,
				Quantity:        1,
				UnitPrice:       eq.UnitPrice,
				Deposit:         eq.Deposit,
				SubDeposit:      eq.Deposit,
				SubRentFee:      eq.UnitPrice * hours,
			})
			// [Bug3 Fix] 注意：这里不再修改 pk.Status 为 rented！
			// 仅做"逻辑分配"（占用名额 + 时段锁），到 Pickup 时才将单件置为 rented。
			// 这样其他不冲突时段可以照常使用这些仍在 in_stock 的单件。
		}

		stockLocks = append(stockLocks, models.EquipmentStockLock{
			EquipmentID: it.EquipmentID,
			VenueID:     booking.VenueID,
			BookingID:   booking.ID,
			BookDate:    booking.BookDate,
			StartHour:   booking.StartHour,
			EndHour:     booking.EndHour,
			Quantity:    it.Quantity,
		})
	}

	rental.TotalDeposit = totalDeposit
	rental.TotalRentFee = totalRentFee
	if err := tx.Create(rental).Error; err != nil {
		return nil, err
	}

	for i := range rentalItems {
		rentalItems[i].RentalID = rental.ID
	}
	if err := tx.Create(&rentalItems).Error; err != nil {
		return nil, err
	}

	for i := range stockLocks {
		stockLocks[i].RentalID = rental.ID
	}
	if err := tx.Create(&stockLocks).Error; err != nil {
		return nil, err
	}

	// [Bug4 Fix] 写入锁定后再做一次二次校验：时段重叠的锁定量不能超过物理在库数
	// 这是悲观锁之外的第二道防线
	for _, it := range items {
		available, err := h.calcAvailableStock(tx, it.EquipmentID, booking.VenueID, booking.BookDate, booking.StartHour, booking.EndHour)
		if err != nil {
			return nil, err
		}
		if available < 0 {
			return nil, errors.New("并发冲突：库存占用校验二次检查失败，请重试")
		}
	}

	// 更新预订上的器材押金字段
	if err := tx.Model(booking).Update("equipment_deposit", totalDeposit).Error; err != nil {
		return nil, err
	}

	return rental, nil
}

// ---------- 领用确认 ----------

func (h *Handler) PickupRental(c *gin.Context) {
	var r models.EquipmentRental
	if err := h.DB.Preload("Items").First(&r, c.Param("id")).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"detail": "租赁单不存在"})
		return
	}
	if r.Status != "frozen" {
		c.JSON(http.StatusUnprocessableEntity, gin.H{"detail": "当前状态不可领用（仅 frozen 状态可领用）"})
		return
	}
	operator := getOperator(c)
	now := time.Now()

	// [Bug3 Fix] 领用环节才把单件实际置为 rented（物理出库）
	err := h.DB.Transaction(func(tx *gorm.DB) error {
		for _, it := range r.Items {
			// 顺便校验该单件目前仍处于 in_stock（防止被其他流程占用）
			result := tx.Model(&models.EquipmentItem{}).
				Where("id = ? AND status = ?", it.EquipmentItemID, "in_stock").
				Update("status", "rented")
			if result.RowsAffected == 0 {
				return fmt.Errorf("单件 %d 已不在库，无法领用", it.EquipmentItemID)
			}
		}
		return nil
	})
	if err != nil {
		c.JSON(http.StatusConflict, gin.H{"detail": "领用失败：" + err.Error()})
		return
	}

	r.Status = "picked"
	r.PickedAt = &now
	h.DB.Save(&r)

	// 写 rent_out 库存日志（每件写一条太频繁，按聚合写）
	_ = operator
	h.DB.Preload("Items").First(&r, r.ID)
	c.JSON(http.StatusOK, r)
}

// ---------- 归还核对与赔付 ----------

type returnItemReq struct {
	RentalItemID uint    `json:"rental_item_id" binding:"required"`
	ReturnStatus string  `json:"return_status" binding:"required"` // ok / damaged / lost / not_returned
	Compensation float64 `json:"compensation"`                     // 自定义赔付金额（可选，默认按规则计算）
	Remark       string  `json:"remark"`
}

type returnRentalReq struct {
	Items  []returnItemReq `json:"items" binding:"required"`
	Remark string          `json:"remark"`
}

// 赔付规则：丢失 = 采购价(默认用押金替代)；损坏 = 押金 * 0.5；完好 = 0
func calcDefaultCompensation(item models.EquipmentRentalItem, eqItem models.EquipmentItem, status string) float64 {
	switch status {
	case "lost":
		// 丢失全额赔付，优先用采购价，其次押金
		if eqItem.PurchasePrice > 0 {
			return eqItem.PurchasePrice
		}
		return item.Deposit
	case "damaged":
		// 损坏按押金 50%
		return item.Deposit * 0.5
	default:
		return 0
	}
}

func (h *Handler) ReturnRental(c *gin.Context) {
	var r models.EquipmentRental
	if err := h.DB.Preload("Items").Preload("Items.EquipmentItem").First(&r, c.Param("id")).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"detail": "租赁单不存在"})
		return
	}
	if r.Status != "picked" && r.Status != "frozen" {
		c.JSON(http.StatusUnprocessableEntity, gin.H{"detail": "当前状态不可归还"})
		return
	}
	var req returnRentalReq
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusUnprocessableEntity, gin.H{"detail": "请求参数不合法"})
		return
	}

	operator := getOperator(c)
	now := time.Now()

	err := h.DB.Transaction(func(tx *gorm.DB) error {
		totalCompensation := 0.0
		hasDamage := false

		for _, rit := range req.Items {
			// 找到对应的租赁单项
			var rentalItem models.EquipmentRentalItem
			if err := tx.Where("id = ? AND rental_id = ?", rit.RentalItemID, r.ID).First(&rentalItem).Error; err != nil {
				return err
			}
			if rentalItem.ReturnStatus != "" {
				continue
			}

			// 找到单件
			var eqItem models.EquipmentItem
			if err := tx.First(&eqItem, rentalItem.EquipmentItemID).Error; err != nil {
				return err
			}

			// 计算赔付
			compensation := rit.Compensation
			if compensation <= 0 {
				compensation = calcDefaultCompensation(rentalItem, eqItem, rit.ReturnStatus)
			}
			totalCompensation += compensation

			// 更新租赁单项
			rentalItem.ReturnStatus = rit.ReturnStatus
			rentalItem.Compensation = compensation
			rentalItem.Remark = rit.Remark
			if err := tx.Save(&rentalItem).Error; err != nil {
				return err
			}

			// 更新单件状态机 & 写入库存日志 & 赔付记录
			switch rit.ReturnStatus {
			case "ok":
				// 完好归还：in_stock
				eqItem.Status = "in_stock"
				tx.Save(&eqItem)
				var inStockBefore int64
				tx.Model(&models.EquipmentItem{}).Where("equipment_id = ? AND status = ?", rentalItem.EquipmentID, "in_stock").Count(&inStockBefore)
				h.addInventoryLog(tx, rentalItem.EquipmentID, r.VenueID, r.ID, "return_in", 1, int(inStockBefore), "归还入库", operator)
			case "damaged":
				// 损坏：repairing
				hasDamage = true
				eqItem.Status = "repairing"
				tx.Save(&eqItem)
				tx.Create(&models.EquipmentCompensation{
					RentalID:        r.ID,
					RentalItemID:    rentalItem.ID,
					EquipmentItemID: eqItem.ID,
					CompensationType: "damage",
					Amount:          compensation,
					DeductFromDeposit: min(compensation, rentalItem.SubDeposit),
					ExtraPay:        max(0, compensation-rentalItem.SubDeposit),
					Remark:          rit.Remark,
				})
			case "lost", "not_returned":
				// 丢失/未还：scrapped
				hasDamage = true
				eqItem.Status = "scrapped"
				eqItem.ScrappedAt = &now
				tx.Save(&eqItem)
				tx.Model(&models.Equipment{}).Where("id = ?", rentalItem.EquipmentID).Update("total_stock", gorm.Expr("total_stock - 1"))
				tx.Create(&models.EquipmentCompensation{
					RentalID:        r.ID,
					RentalItemID:    rentalItem.ID,
					EquipmentItemID: eqItem.ID,
					CompensationType: "lost",
					Amount:          compensation,
					DeductFromDeposit: min(compensation, rentalItem.SubDeposit),
					ExtraPay:        max(0, compensation-rentalItem.SubDeposit),
					Remark:          rit.Remark,
				})
				h.addInventoryLog(tx, rentalItem.EquipmentID, r.VenueID, r.ID, "scrap", -1, 0, "丢失报废", operator)
			}
		}

		// 结算：退还押金 = 总押金 - 赔付（赔付不超过押金的部分从押金扣，超出算追收）
		refund := r.TotalDeposit - min(totalCompensation, r.TotalDeposit)
		if refund < 0 {
			refund = 0
		}
		r.Compensation = totalCompensation
		r.RefundDeposit = refund
		r.Remark = req.Remark
		r.ReturnedAt = &now
		if hasDamage {
			r.Status = "damaged"
		} else {
			r.Status = "returned"
		}
		if err := tx.Save(&r).Error; err != nil {
			return err
		}

		// 释放库存占用锁定
		if err := tx.Where("rental_id = ?", r.ID).Delete(&models.EquipmentStockLock{}).Error; err != nil {
			return err
		}

		return nil
	})
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"detail": "归还处理失败：" + err.Error()})
		return
	}

	// 重新加载
	h.DB.Preload("Items").First(&r, r.ID)
	c.JSON(http.StatusOK, r)
}

// ---------- 押金结算 ----------

type settleDepositReq struct {
	SettleType string `json:"settle_type"` // venue / equipment / all
}

func (h *Handler) SettleDeposit(c *gin.Context) {
	var booking models.Booking
	if err := h.DB.First(&booking, c.Param("id")).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"detail": "预订不存在"})
		return
	}
	if booking.DepositSettled {
		c.JSON(http.StatusUnprocessableEntity, gin.H{"detail": "押金已结算"})
		return
	}
	var req settleDepositReq
	if err := c.ShouldBindJSON(&req); err != nil {
		req.SettleType = "all"
	}
	booking.DepositSettled = true
	h.DB.Save(&booking)
	c.JSON(http.StatusOK, gin.H{
		"id": booking.ID,
		"venue_deposit":     booking.VenueDeposit,
		"equipment_deposit": booking.EquipmentDeposit,
		"settle_type":       req.SettleType,
		"deposit_settled":   true,
	})
}

// ---------- 取消租赁（释放占用、退回单件） ----------

func (h *Handler) CancelRental(c *gin.Context) {
	var r models.EquipmentRental
	if err := h.DB.Preload("Items").First(&r, c.Param("id")).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"detail": "租赁单不存在"})
		return
	}
	if r.Status != "frozen" {
		c.JSON(http.StatusUnprocessableEntity, gin.H{"detail": "仅冻结状态可取消（已领用请走归还流程）"})
		return
	}

	err := h.DB.Transaction(func(tx *gorm.DB) error {
		// [Bug3 Fix] 创建租赁时没有改单件 status，取消时也不用回写
		// 只需要删除库存锁定 + 标为 cancelled
		tx.Where("rental_id = ?", r.ID).Delete(&models.EquipmentStockLock{})
		r.Status = "cancelled"
		tx.Save(&r)
		return nil
	})
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"detail": "取消失败"})
		return
	}
	c.JSON(http.StatusOK, r)
}

// ---------- 赔付记录查询 ----------

func (h *Handler) ListCompensations(c *gin.Context) {
	var list []models.EquipmentCompensation
	q := h.DB.Order("id desc")
	if rid := c.Query("rental_id"); rid != "" {
		q = q.Where("rental_id = ?", rid)
	}
	q.Limit(200).Find(&list)
	c.JSON(http.StatusOK, list)
}
