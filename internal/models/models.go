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
	ID              uint      `gorm:"primaryKey" json:"id"`
	VenueID         uint      `gorm:"index" json:"venue_id"`
	CustomerName    string    `gorm:"size:64" json:"customer_name"`
	Phone           string    `gorm:"size:32" json:"phone"`
	BookDate        string    `gorm:"size:10;index" json:"book_date"` // YYYY-MM-DD
	StartHour       int       `json:"start_hour"`
	EndHour         int       `json:"end_hour"`
	Amount          float64   `json:"amount"`
	VenueDeposit    float64   `json:"venue_deposit"`      // 场地押金
	EquipmentDeposit float64  `json:"equipment_deposit"`  // 器材押金
	DepositSettled  bool      `json:"deposit_settled"`    // 押金是否已结算
	Status          string    `gorm:"size:16" json:"status"` // booked / cancelled / completed
	CreatedAt       time.Time `json:"created_at"`
}

// ---------- 器材 ----------

// EquipmentCategory 器材类别。
type EquipmentCategory struct {
	ID          uint      `gorm:"primaryKey" json:"id"`
	Name        string    `gorm:"size:64;uniqueIndex" json:"name"` // 羽毛球拍 / 篮球 / 护腕 ...
	Description string    `gorm:"size:255" json:"description"`
	CreatedAt   time.Time `json:"created_at"`
}

// Equipment 器材（按类别+场馆聚合，管理单价、押金、总库存、预警阈值）。
type Equipment struct {
	ID           uint      `gorm:"primaryKey" json:"id"`
	CategoryID   uint      `gorm:"index" json:"category_id"`
	VenueID      uint      `gorm:"index" json:"venue_id"`
	Name         string    `gorm:"size:128" json:"name"`
	UnitPrice    float64   `json:"unit_price"`    // 单件租赁单价（每小时/次）
	Deposit      float64   `json:"deposit"`       // 单件押金
	TotalStock   int       `json:"total_stock"`   // 总库存（含所有状态）
	WarningStock int       `json:"warning_stock"` // 低库存预警阈值
	Status       string    `gorm:"size:16" json:"status"` // active / inactive
	CreatedAt    time.Time `json:"created_at"`

	Category *EquipmentCategory `gorm:"foreignKey:CategoryID" json:"category,omitempty"`
	Venue    *Venue             `gorm:"foreignKey:VenueID" json:"venue,omitempty"`
}

// EquipmentItem 单件器材（按编号追踪每一件的状态流转）。
// 状态机: in_stock(在库) -> rented(租出) -> in_stock / repairing(维修) / scrapped(报废)
//       repairing -> in_stock / scrapped
type EquipmentItem struct {
	ID          uint      `gorm:"primaryKey" json:"id"`
	EquipmentID uint      `gorm:"index" json:"equipment_id"`
	SerialNo    string    `gorm:"size:64;uniqueIndex" json:"serial_no"` // 唯一编号
	Status      string    `gorm:"size:16;index" json:"status"`         // in_stock / rented / repairing / scrapped
	Location    string    `gorm:"size:128" json:"location"`            // 存放位置
	Remark      string    `gorm:"size:255" json:"remark"`
	PurchasePrice float64 `json:"purchase_price"`                      // 采购成本（用于报废核算）
	PurchaseDate  *time.Time `json:"purchase_date"`
	CreatedAt   time.Time `json:"created_at"`
	ScrappedAt  *time.Time `json:"scrapped_at,omitempty"`

	Equipment *Equipment `gorm:"foreignKey:EquipmentID" json:"equipment,omitempty"`
}

// ---------- 租赁 ----------

// EquipmentRental 器材租赁单，与预订关联。
// 状态: frozen(已冻结/待领用) -> picked(已领用) -> returned(已归还) / damaged(损坏赔付中) / cancelled
type EquipmentRental struct {
	ID             uint      `gorm:"primaryKey" json:"id"`
	BookingID      uint      `gorm:"index" json:"booking_id"`
	VenueID        uint      `gorm:"index" json:"venue_id"`
	TotalDeposit   float64   `json:"total_deposit"`    // 总押金
	TotalRentFee   float64   `json:"total_rent_fee"`   // 总租金
	Compensation   float64   `json:"compensation"`     // 已赔付金额
	RefundDeposit  float64   `json:"refund_deposit"`   // 已退押金
	Status         string    `gorm:"size:16" json:"status"` // frozen / picked / returned / damaged / cancelled
	Remark         string    `gorm:"size:255" json:"remark"`
	PickedAt       *time.Time `json:"picked_at,omitempty"`
	ReturnedAt     *time.Time `json:"returned_at,omitempty"`
	CreatedAt      time.Time `json:"created_at"`

	Items []EquipmentRentalItem `gorm:"foreignKey:RentalID" json:"items,omitempty"`
}

// EquipmentRentalItem 租赁单项，每件器材对应一条。
type EquipmentRentalItem struct {
	ID              uint      `gorm:"primaryKey" json:"id"`
	RentalID        uint      `gorm:"index" json:"rental_id"`
	EquipmentID     uint      `gorm:"index" json:"equipment_id"`
	EquipmentItemID uint      `gorm:"index" json:"equipment_item_id"` // 具体分配到的单件
	Quantity        int       `json:"quantity"`                        // 本项数量（聚合展示用，单件则为1）
	UnitPrice       float64   `json:"unit_price"`
	Deposit         float64   `json:"deposit"`       // 单件押金
	SubDeposit      float64   `json:"sub_deposit"`   // 本项押金合计
	SubRentFee      float64   `json:"sub_rent_fee"`  // 本项租金合计
	ReturnStatus    string    `gorm:"size:16" json:"return_status"` // ok / damaged / lost / not_returned
	Compensation    float64   `json:"compensation"`  // 本项赔付
	Remark          string    `gorm:"size:255" json:"remark"`
	CreatedAt       time.Time `json:"created_at"`

	Equipment     *Equipment     `gorm:"foreignKey:EquipmentID" json:"equipment,omitempty"`
	EquipmentItem *EquipmentItem `gorm:"foreignKey:EquipmentItemID" json:"equipment_item,omitempty"`
}

// ---------- 库存占用（时段级） ----------

// EquipmentStockLock 器材库存时段占用记录，用于并发一致校验。
// 每次预订租赁时按 (equipment_id, date, start_hour, end_hour) 记录占用数量，
// 通过数据库唯一索引+事务实现并发下不超额。
type EquipmentStockLock struct {
	ID          uint      `gorm:"primaryKey" json:"id"`
	EquipmentID uint      `gorm:"index" json:"equipment_id"`
	VenueID     uint      `gorm:"index" json:"venue_id"`
	BookingID   uint      `gorm:"index" json:"booking_id"`
	RentalID    uint      `gorm:"index" json:"rental_id"`
	BookDate    string    `gorm:"size:10;index" json:"book_date"`
	StartHour   int       `json:"start_hour"`
	EndHour     int       `json:"end_hour"`
	Quantity    int       `json:"quantity"`
	CreatedAt   time.Time `json:"created_at"`
}

// ---------- 库存变动日志 ----------

// InventoryLog 库存变动日志（入库/出库/调拨/报废/盘点损益）。
type InventoryLog struct {
	ID          uint      `gorm:"primaryKey" json:"id"`
	EquipmentID uint      `gorm:"index" json:"equipment_id"`
	VenueID     uint      `gorm:"index" json:"venue_id"`
	ChangeType  string    `gorm:"size:32" json:"change_type"` // purchase_in / rent_out / return_in / transfer_in / transfer_out / stock_check / scrap
	Quantity    int       `json:"quantity"`                    // 正数入库，负数出库
	BeforeStock int       `json:"before_stock"`
	AfterStock  int       `json:"after_stock"`
	RelatedID   uint      `gorm:"index" json:"related_id"` // 关联采购单/调拨单/租赁单/盘点单ID
	Remark      string    `gorm:"size:255" json:"remark"`
	CreatedAt   time.Time `json:"created_at"`
	Operator    string    `gorm:"size:64" json:"operator"`
}

// ---------- 跨场馆调拨 ----------

// EquipmentTransfer 跨场馆调拨单。
// 状态: pending(在途) -> completed(已入库) / cancelled
type EquipmentTransfer struct {
	ID            uint      `gorm:"primaryKey" json:"id"`
	FromVenueID   uint      `gorm:"index" json:"from_venue_id"`
	ToVenueID     uint      `gorm:"index" json:"to_venue_id"`
	Status        string    `gorm:"size:16" json:"status"` // pending / completed / cancelled
	Remark        string    `gorm:"size:255" json:"remark"`
	CreatedAt     time.Time `json:"created_at"`
	CompletedAt   *time.Time `json:"completed_at,omitempty"`

	Items []EquipmentTransferItem `gorm:"foreignKey:TransferID" json:"items,omitempty"`
}

type EquipmentTransferItem struct {
	ID            uint `gorm:"primaryKey" json:"id"`
	TransferID    uint `gorm:"index" json:"transfer_id"`
	EquipmentID   uint `gorm:"index" json:"equipment_id"`
	Quantity      int  `json:"quantity"`
}

// ---------- 盘点 ----------

// StockCheck 盘点单。
type StockCheck struct {
	ID        uint      `gorm:"primaryKey" json:"id"`
	VenueID   uint      `gorm:"index" json:"venue_id"`
	Status    string    `gorm:"size:16" json:"status"` // draft / confirmed
	Remark    string    `gorm:"size:255" json:"remark"`
	CreatedAt time.Time `json:"created_at"`
	ConfirmedAt *time.Time `json:"confirmed_at,omitempty"`

	Items []StockCheckItem `gorm:"foreignKey:StockCheckID" json:"items,omitempty"`
}

type StockCheckItem struct {
	ID            uint `gorm:"primaryKey" json:"id"`
	StockCheckID  uint `gorm:"index" json:"stock_check_id"`
	EquipmentID   uint `gorm:"index" json:"equipment_id"`
	SystemStock   int  `json:"system_stock"`   // 系统账面在库数
	PhysicalStock int  `json:"physical_stock"` // 实物盘点数
	Diff          int  `json:"diff"`           // 差异
	Remark        string `gorm:"size:255" json:"remark"`
}

// ---------- 采购入库 ----------

// EquipmentPurchase 采购入库单。
type EquipmentPurchase struct {
	ID          uint      `gorm:"primaryKey" json:"id"`
	VenueID     uint      `gorm:"index" json:"venue_id"`
	Supplier    string    `gorm:"size:128" json:"supplier"`
	TotalAmount float64   `json:"total_amount"`
	Remark      string    `gorm:"size:255" json:"remark"`
	CreatedAt   time.Time `json:"created_at"`

	Items []EquipmentPurchaseItem `gorm:"foreignKey:PurchaseID" json:"items,omitempty"`
}

type EquipmentPurchaseItem struct {
	ID            uint      `gorm:"primaryKey" json:"id"`
	PurchaseID    uint      `gorm:"index" json:"purchase_id"`
	EquipmentID   uint      `gorm:"index" json:"equipment_id"`
	Quantity      int       `json:"quantity"`
	UnitPrice     float64   `json:"unit_price"`
	Subtotal      float64   `json:"subtotal"`
}

// ---------- 赔付 ----------

// EquipmentCompensation 赔付记录（归还时损坏/丢失产生）。
type EquipmentCompensation struct {
	ID              uint      `gorm:"primaryKey" json:"id"`
	RentalID        uint      `gorm:"index" json:"rental_id"`
	RentalItemID    uint      `gorm:"index" json:"rental_item_id"`
	EquipmentItemID uint      `gorm:"index" json:"equipment_item_id"`
	CompensationType string   `gorm:"size:16" json:"compensation_type"` // damage / lost
	Amount          float64   `json:"amount"`          // 赔付金额
	DeductFromDeposit float64 `json:"deduct_from_deposit"` // 从押金扣除
	ExtraPay        float64   `json:"extra_pay"`       // 额外支付（押金不足时）
	Remark          string    `gorm:"size:255" json:"remark"`
	CreatedAt       time.Time `json:"created_at"`
}
