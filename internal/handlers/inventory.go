package handlers

import (
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	"venue-booking-admin/internal/models"
)

// ---------- 跨场馆调拨 ----------

type transferItem struct {
	EquipmentID uint `json:"equipment_id" binding:"required"`
	Quantity    int  `json:"quantity" binding:"required,min=1"`
}

type transferReq struct {
	FromVenueID uint           `json:"from_venue_id" binding:"required"`
	ToVenueID   uint           `json:"to_venue_id" binding:"required"`
	Items       []transferItem `json:"items" binding:"required,min=1"`
	Remark      string         `json:"remark"`
}

func (h *Handler) ListTransfers(c *gin.Context) {
	var list []models.EquipmentTransfer
	q := h.DB.Preload("Items").Order("id desc")
	if vid := c.Query("from_venue_id"); vid != "" {
		q = q.Where("from_venue_id = ?", vid)
	}
	if vid := c.Query("to_venue_id"); vid != "" {
		q = q.Where("to_venue_id = ?", vid)
	}
	if status := c.Query("status"); status != "" {
		q = q.Where("status = ?", status)
	}
	q.Find(&list)
	c.JSON(http.StatusOK, list)
}

func (h *Handler) CreateTransfer(c *gin.Context) {
	var req transferReq
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusUnprocessableEntity, gin.H{"detail": "请求参数不合法"})
		return
	}
	if req.FromVenueID == req.ToVenueID {
		c.JSON(http.StatusUnprocessableEntity, gin.H{"detail": "调出场馆与调入场馆不能相同"})
		return
	}
	operator := getOperator(c)

	err := h.DB.Transaction(func(tx *gorm.DB) error {
		transfer := &models.EquipmentTransfer{
			FromVenueID: req.FromVenueID,
			ToVenueID:   req.ToVenueID,
			Status:      "pending",
			Remark:      req.Remark,
		}
		if err := tx.Create(transfer).Error; err != nil {
			return err
		}

		for _, it := range req.Items {
			// 校验调出馆有足够的在库数量
			var inStock int64
			tx.Model(&models.EquipmentItem{}).
				Joins("JOIN equipments ON equipments.id = equipment_items.equipment_id").
				Where("equipment_items.equipment_id = ? AND equipments.venue_id = ? AND equipment_items.status = ?",
					it.EquipmentID, req.FromVenueID, "in_stock").Count(&inStock)
			if int(inStock) < it.Quantity {
				return errors.New("insufficient stock in source venue")
			}

			// 选取并标记为"在途"（用状态 rented 不太对，此处借用 repairing 作为临时在途状态；
			// 更规范做法：但状态机有限，我们改为先将这些单件从 from 的 venue 库存里扣减，
			// 实际入库时再在 to 的 venue 生成器材记录或直接把单件移过去。
			// 简化实现：先把单件状态改成 rented 视为锁定，完成调拨时迁移 venue。）
			var pickedItems []models.EquipmentItem
			tx.Joins("JOIN equipments ON equipments.id = equipment_items.equipment_id").
				Where("equipment_items.equipment_id = ? AND equipments.venue_id = ? AND equipment_items.status = ?",
					it.EquipmentID, req.FromVenueID, "in_stock").
				Limit(it.Quantity).Find(&pickedItems)
			for _, pi := range pickedItems {
				tx.Model(&pi).Update("status", "repairing") // 借用 repairing 表示"调拨在途"
			}

			tx.Create(&models.EquipmentTransferItem{
				TransferID:  transfer.ID,
				EquipmentID: it.EquipmentID,
				Quantity:    it.Quantity,
			})

			h.addInventoryLog(tx, it.EquipmentID, req.FromVenueID, transfer.ID, "transfer_out", -it.Quantity, int(inStock), "调拨出库", operator)
		}
		return nil
	})
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"detail": "创建调拨失败：调出馆库存不足或参数错误"})
		return
	}
	var transfer models.EquipmentTransfer
	h.DB.Preload("Items").Last(&transfer)
	c.JSON(http.StatusCreated, transfer)
}

func (h *Handler) CompleteTransfer(c *gin.Context) {
	var transfer models.EquipmentTransfer
	if err := h.DB.Preload("Items").First(&transfer, c.Param("id")).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"detail": "调拨单不存在"})
		return
	}
	if transfer.Status != "pending" {
		c.JSON(http.StatusUnprocessableEntity, gin.H{"detail": "仅在途调拨可入库"})
		return
	}
	operator := getOperator(c)
	now := time.Now()

	err := h.DB.Transaction(func(tx *gorm.DB) error {
		for _, it := range transfer.Items {
			// 1) 先从调出馆找到对应数量的"在途"单件
			var inTransitItems []models.EquipmentItem
			tx.Joins("JOIN equipments ON equipments.id = equipment_items.equipment_id").
				Where("equipment_items.equipment_id = ? AND equipments.venue_id = ? AND equipment_items.status = ?",
					it.EquipmentID, transfer.FromVenueID, "repairing").
				Limit(it.Quantity).Find(&inTransitItems)

			// 2) 看目标场馆是否已有该器材（按 category + name 聚合判断，简化：直接查找同 name+to_venue）
			var targetEq models.Equipment
			err := tx.Where("venue_id = ? AND name = ?", transfer.ToVenueID, "").
				Joins("JOIN equipments src ON src.id = ?", it.EquipmentID).
				First(&targetEq).Error
			if err != nil {
				// 不存在则克隆一条器材记录到目标场馆
				var srcEq models.Equipment
				if err := tx.First(&srcEq, it.EquipmentID).Error; err != nil {
					return err
				}
				targetEq = models.Equipment{
					CategoryID:   srcEq.CategoryID,
					VenueID:      transfer.ToVenueID,
					Name:         srcEq.Name,
					UnitPrice:    srcEq.UnitPrice,
					Deposit:      srcEq.Deposit,
					WarningStock: srcEq.WarningStock,
					Status:       srcEq.Status,
					TotalStock:   0,
				}
				if err := tx.Create(&targetEq).Error; err != nil {
					return err
				}
			}

			// 3) 将单件的 equipment_id 指向目标场馆的聚合记录，并状态恢复 in_stock
			for _, pi := range inTransitItems {
				tx.Model(&pi).Updates(map[string]interface{}{
					"equipment_id": targetEq.ID,
					"status":       "in_stock",
				})
			}
			// 4) 更新目标聚合的 total_stock
			tx.Model(&targetEq).Update("total_stock", gorm.Expr("total_stock + ?", it.Quantity))
			// 5) 源聚合 total_stock 扣减
			tx.Model(&models.Equipment{}).Where("id = ?", it.EquipmentID).Update("total_stock", gorm.Expr("total_stock - ?", it.Quantity))

			// 6) 调入馆库存日志
			var targetInStock int64
			tx.Model(&models.EquipmentItem{}).Where("equipment_id = ? AND status = ?", targetEq.ID, "in_stock").Count(&targetInStock)
			h.addInventoryLog(tx, targetEq.ID, transfer.ToVenueID, transfer.ID, "transfer_in", it.Quantity, int(targetInStock), "调拨入库", operator)
		}

		transfer.Status = "completed"
		transfer.CompletedAt = &now
		tx.Save(&transfer)
		return nil
	})
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"detail": "调拨入库失败：" + err.Error()})
		return
	}
	c.JSON(http.StatusOK, transfer)
}

func (h *Handler) CancelTransfer(c *gin.Context) {
	var transfer models.EquipmentTransfer
	if err := h.DB.Preload("Items").First(&transfer, c.Param("id")).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"detail": "调拨单不存在"})
		return
	}
	if transfer.Status != "pending" {
		c.JSON(http.StatusUnprocessableEntity, gin.H{"detail": "仅在途调拨可取消"})
		return
	}
	operator := getOperator(c)

	h.DB.Transaction(func(tx *gorm.DB) error {
		for _, it := range transfer.Items {
			tx.Model(&models.EquipmentItem{}).
				Joins("JOIN equipments ON equipments.id = equipment_items.equipment_id").
				Where("equipment_items.equipment_id = ? AND equipments.venue_id = ? AND equipment_items.status = ?",
					it.EquipmentID, transfer.FromVenueID, "repairing").
				Limit(it.Quantity).
				Update("status", "in_stock")

			var inStockAfter int64
			tx.Model(&models.EquipmentItem{}).
				Joins("JOIN equipments ON equipments.id = equipment_items.equipment_id").
				Where("equipment_items.equipment_id = ? AND equipments.venue_id = ? AND equipment_items.status = ?",
					it.EquipmentID, transfer.FromVenueID, "in_stock").Count(&inStockAfter)
			h.addInventoryLog(tx, it.EquipmentID, transfer.FromVenueID, transfer.ID, "transfer_in", it.Quantity, int(inStockAfter), "调拨取消回库", operator)
		}
		transfer.Status = "cancelled"
		tx.Save(&transfer)
		return nil
	})
	c.JSON(http.StatusOK, transfer)
}

// ---------- 盘点 ----------

type stockCheckItemReq struct {
	EquipmentID   uint `json:"equipment_id" binding:"required"`
	PhysicalStock int  `json:"physical_stock" binding:"required,min=0"`
	Remark        string `json:"remark"`
}

type stockCheckReq struct {
	VenueID uint                `json:"venue_id" binding:"required"`
	Items   []stockCheckItemReq `json:"items" binding:"required,min=1"`
	Remark  string              `json:"remark"`
	Status  string              `json:"status"` // draft / confirmed
}

func (h *Handler) ListStockChecks(c *gin.Context) {
	var list []models.StockCheck
	q := h.DB.Preload("Items").Order("id desc")
	if vid := c.Query("venue_id"); vid != "" {
		q = q.Where("venue_id = ?", vid)
	}
	if status := c.Query("status"); status != "" {
		q = q.Where("status = ?", status)
	}
	q.Find(&list)
	c.JSON(http.StatusOK, list)
}

func (h *Handler) CreateStockCheck(c *gin.Context) {
	var req stockCheckReq
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusUnprocessableEntity, gin.H{"detail": "请求参数不合法"})
		return
	}
	status := req.Status
	if status == "" {
		status = "draft"
	}
	operator := getOperator(c)

	var sc models.StockCheck
	err := h.DB.Transaction(func(tx *gorm.DB) error {
		sc = models.StockCheck{
			VenueID: req.VenueID,
			Status:  status,
			Remark:  req.Remark,
		}
		if err := tx.Create(&sc).Error; err != nil {
			return err
		}
		for _, it := range req.Items {
			var eq models.Equipment
			if err := tx.Where("id = ? AND venue_id = ?", it.EquipmentID, req.VenueID).First(&eq).Error; err != nil {
				return err
			}
			var systemStock int64
			tx.Model(&models.EquipmentItem{}).Where("equipment_id = ? AND status = ?", it.EquipmentID, "in_stock").Count(&systemStock)
			diff := it.PhysicalStock - int(systemStock)
			tx.Create(&models.StockCheckItem{
				StockCheckID:  sc.ID,
				EquipmentID:   it.EquipmentID,
				SystemStock:   int(systemStock),
				PhysicalStock: it.PhysicalStock,
				Diff:          diff,
				Remark:        it.Remark,
			})

			// 如直接确认，则同时调账：盘盈则新建单件，盘亏则报废对应数量单件
			if status == "confirmed" && diff != 0 {
				if diff > 0 {
					// 盘盈：生成 diff 个新单件
					for i := 0; i < diff; i++ {
						tx.Create(&models.EquipmentItem{
							EquipmentID: it.EquipmentID,
							SerialNo:    fmt.Sprintf("SC%s-%d-%s", time.Now().Format("060102150405"), it.EquipmentID, randomSuffix(i)),
							Status:      "in_stock",
							Remark:      "盘点盘盈入库",
						})
					}
					tx.Model(&eq).Update("total_stock", gorm.Expr("total_stock + ?", diff))
					h.addInventoryLog(tx, it.EquipmentID, req.VenueID, sc.ID, "stock_check", diff, int(systemStock), "盘点盘盈", operator)
				} else {
					// 盘亏：报废 |diff| 件
					lossQty := -diff
					var itemsToLoss []models.EquipmentItem
					tx.Where("equipment_id = ? AND status = ?", it.EquipmentID, "in_stock").Limit(lossQty).Find(&itemsToLoss)
					now := time.Now()
					for _, it2 := range itemsToLoss {
						tx.Model(&it2).Updates(map[string]interface{}{"status": "scrapped", "scrapped_at": &now})
					}
					tx.Model(&eq).Update("total_stock", gorm.Expr("total_stock - ?", lossQty))
					h.addInventoryLog(tx, it.EquipmentID, req.VenueID, sc.ID, "stock_check", -lossQty, int(systemStock), "盘点盘亏", operator)
				}
			}
		}
		if status == "confirmed" {
			now := time.Now()
			sc.ConfirmedAt = &now
			tx.Save(&sc)
		}
		return nil
	})
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"detail": "创建盘点失败：" + err.Error()})
		return
	}
	h.DB.Preload("Items").First(&sc, sc.ID)
	c.JSON(http.StatusCreated, sc)
}

func randomSuffix(i int) string {
	return fmt.Sprintf("%03d", i)
}

func (h *Handler) ConfirmStockCheck(c *gin.Context) {
	var sc models.StockCheck
	if err := h.DB.Preload("Items").First(&sc, c.Param("id")).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"detail": "盘点单不存在"})
		return
	}
	if sc.Status == "confirmed" {
		c.JSON(http.StatusUnprocessableEntity, gin.H{"detail": "盘点单已确认"})
		return
	}
	operator := getOperator(c)
	now := time.Now()

	err := h.DB.Transaction(func(tx *gorm.DB) error {
		for _, it := range sc.Items {
			if it.Diff == 0 {
				continue
			}
			if it.Diff > 0 {
				for i := 0; i < it.Diff; i++ {
					tx.Create(&models.EquipmentItem{
						EquipmentID: it.EquipmentID,
						SerialNo:    fmt.Sprintf("SC%s-%d-%s", time.Now().Format("060102150405"), it.EquipmentID, randomSuffix(i)),
						Status:      "in_stock",
						Remark:      "盘点盘盈入库",
					})
				}
				tx.Model(&models.Equipment{}).Where("id = ?", it.EquipmentID).Update("total_stock", gorm.Expr("total_stock + ?", it.Diff))
				h.addInventoryLog(tx, it.EquipmentID, sc.VenueID, sc.ID, "stock_check", it.Diff, it.SystemStock, "盘点盘盈", operator)
			} else {
				lossQty := -it.Diff
				var lossItems []models.EquipmentItem
				tx.Where("equipment_id = ? AND status = ?", it.EquipmentID, "in_stock").Limit(lossQty).Find(&lossItems)
				for _, li := range lossItems {
					tx.Model(&li).Updates(map[string]interface{}{"status": "scrapped", "scrapped_at": &now})
				}
				tx.Model(&models.Equipment{}).Where("id = ?", it.EquipmentID).Update("total_stock", gorm.Expr("total_stock - ?", lossQty))
				h.addInventoryLog(tx, it.EquipmentID, sc.VenueID, sc.ID, "stock_check", -lossQty, it.SystemStock, "盘点盘亏", operator)
			}
		}
		sc.Status = "confirmed"
		sc.ConfirmedAt = &now
		tx.Save(&sc)
		return nil
	})
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"detail": "确认盘点失败"})
		return
	}
	c.JSON(http.StatusOK, sc)
}

// ---------- 采购入库 ----------

type purchaseItemReq struct {
	EquipmentID uint    `json:"equipment_id" binding:"required"`
	Quantity    int     `json:"quantity" binding:"required,min=1"`
	UnitPrice   float64 `json:"unit_price"`
}

type purchaseReq struct {
	VenueID  uint              `json:"venue_id" binding:"required"`
	Supplier string            `json:"supplier"`
	Items    []purchaseItemReq `json:"items" binding:"required,min=1"`
	Remark   string            `json:"remark"`
}

func (h *Handler) ListPurchases(c *gin.Context) {
	var list []models.EquipmentPurchase
	q := h.DB.Preload("Items").Order("id desc")
	if vid := c.Query("venue_id"); vid != "" {
		q = q.Where("venue_id = ?", vid)
	}
	q.Find(&list)
	c.JSON(http.StatusOK, list)
}

func (h *Handler) CreatePurchase(c *gin.Context) {
	var req purchaseReq
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusUnprocessableEntity, gin.H{"detail": "请求参数不合法"})
		return
	}
	operator := getOperator(c)
	now := time.Now()

	var purchase models.EquipmentPurchase
	err := h.DB.Transaction(func(tx *gorm.DB) error {
		total := 0.0
		purchase = models.EquipmentPurchase{
			VenueID:  req.VenueID,
			Supplier: req.Supplier,
			Remark:   req.Remark,
		}
		if err := tx.Create(&purchase).Error; err != nil {
			return err
		}
		for _, it := range req.Items {
			var eq models.Equipment
			if err := tx.Where("id = ? AND venue_id = ?", it.EquipmentID, req.VenueID).First(&eq).Error; err != nil {
				return err
			}
			subtotal := float64(it.Quantity) * it.UnitPrice
			total += subtotal
			tx.Create(&models.EquipmentPurchaseItem{
				PurchaseID:  purchase.ID,
				EquipmentID: it.EquipmentID,
				Quantity:    it.Quantity,
				UnitPrice:   it.UnitPrice,
				Subtotal:    subtotal,
			})
			// 生成 it.Quantity 个新单件
			for i := 0; i < it.Quantity; i++ {
				tx.Create(&models.EquipmentItem{
					EquipmentID:   it.EquipmentID,
					SerialNo:      fmt.Sprintf("PUR%s-%d-%s", time.Now().Format("060102150405"), it.EquipmentID, randomSuffix(i)),
					Status:        "in_stock",
					PurchasePrice: it.UnitPrice,
					PurchaseDate:  &now,
					Remark:        "采购入库",
				})
			}
			// 更新聚合库存
			tx.Model(&eq).Update("total_stock", gorm.Expr("total_stock + ?", it.Quantity))

			var inStockAfter int64
			tx.Model(&models.EquipmentItem{}).Where("equipment_id = ? AND status = ?", it.EquipmentID, "in_stock").Count(&inStockAfter)
			h.addInventoryLog(tx, it.EquipmentID, req.VenueID, purchase.ID, "purchase_in", it.Quantity, int(inStockAfter)-it.Quantity, "采购入库", operator)
		}
		purchase.TotalAmount = total
		tx.Save(&purchase)
		return nil
	})
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"detail": "采购入库失败：" + err.Error()})
		return
	}
	h.DB.Preload("Items").First(&purchase, purchase.ID)
	c.JSON(http.StatusCreated, purchase)
}

// ---------- 单件报废登记 ----------

type scrapItemReq struct {
	EquipmentItemID uint   `json:"equipment_item_id" binding:"required"`
	Remark          string `json:"remark"`
}

type scrapReq struct {
	Items  []scrapItemReq `json:"items" binding:"required,min=1"`
	Remark string         `json:"remark"`
}

func (h *Handler) ScrapEquipmentItems(c *gin.Context) {
	var req scrapReq
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusUnprocessableEntity, gin.H{"detail": "请求参数不合法"})
		return
	}
	operator := getOperator(c)
	now := time.Now()

	err := h.DB.Transaction(func(tx *gorm.DB) error {
		for _, it := range req.Items {
			var item models.EquipmentItem
			if err := tx.First(&item, it.EquipmentItemID).Error; err != nil {
				return err
			}
			if item.Status == "rented" {
				return errors.New("cannot scrap rented equipment item")
			}
			if item.Status == "scrapped" {
				continue
			}
			item.Status = "scrapped"
			item.ScrappedAt = &now
			item.Remark = it.Remark
			tx.Save(&item)
			tx.Model(&models.Equipment{}).Where("id = ?", item.EquipmentID).Update("total_stock", gorm.Expr("total_stock - 1"))

			var eq models.Equipment
			tx.First(&eq, item.EquipmentID)
			var inStockBefore int64
			tx.Model(&models.EquipmentItem{}).Where("equipment_id = ? AND status = ?", item.EquipmentID, "in_stock").Count(&inStockBefore)
			h.addInventoryLog(tx, item.EquipmentID, eq.VenueID, 0, "scrap", -1, int(inStockBefore)+1, it.Remark, operator)
		}
		return nil
	})
	if err != nil {
		c.JSON(http.StatusUnprocessableEntity, gin.H{"detail": "报废失败：可能存在已租出单件"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"status": "ok", "count": len(req.Items)})
}
