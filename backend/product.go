package main

import (
	"context"
	"database/sql"
	"net/http"
	"strings"
	"unicode/utf8"
)

type Product struct {
	ID                string `json:"id"`
	Name              string `json:"name"`
	Description       string `json:"description"`
	Details           string `json:"details"`
	Price             int64  `json:"price"`
	SalePrice         *int64 `json:"sale_price,omitempty"`
	OfferName         string `json:"offer_name,omitempty"`
	Currency          string `json:"currency"`
	InventoryStrategy string `json:"inventory_strategy"`
	InventoryCount    int    `json:"inventory_count"`
	StockUnlimited    bool   `json:"stock_unlimited"`
	DeliveryMode      string `json:"delivery_mode"`
	DeliveryNote      string `json:"delivery_note"`
	BuyerNoteLabel    string `json:"buyer_note_label"`
	BuyerNoteRequired bool   `json:"buyer_note_required"`
	MinQuantity       int    `json:"min_quantity"`
	MaxQuantity       int    `json:"max_quantity"`
	// 多种交付类型共享一个商品时标为 mixed，实际交付按每份库存快照处理。
	Kind      string `json:"kind"`
	Active    bool   `json:"active"`
	Hidden    bool   `json:"hidden"`
	Available int    `json:"available"`
	Reserved  int    `json:"reserved"`
	Sold      int    `json:"sold"`
	UpdatedAt int64  `json:"updated_at"`
}

const productSelect = `SELECT p.id,p.name,p.description,p.details,p.price,p.currency,
 CASE WHEN p.delivery_mode='manual' THEN 'manual' ELSE COALESCE(b.kind,'') END,p.active,p.hidden,p.updated_at,
 COALESCE(b.available,0),COALESCE(b.reserved,0),COALESCE(b.sold,0),p.inventory_strategy,COALESCE(b.pool_count,0),COALESCE(b.unlimited,0),
 p.delivery_mode,p.delivery_note,p.buyer_note_label,p.buyer_note_required,p.min_quantity,p.max_quantity
 FROM products p LEFT JOIN (
 SELECT ip.product_id,COUNT(*) pool_count,CASE WHEN COUNT(DISTINCT i.kind)=1 THEN MAX(i.kind) ELSE 'mixed' END kind,
 MAX(i.kind='static' AND i.content<>'') unlimited,
 SUM(COALESCE(s.available,0)) available,SUM(COALESCE(s.reserved,0)) reserved,SUM(COALESCE(s.sold,0)) sold
 FROM inventory_products ip JOIN inventories i ON i.id=ip.inventory_id AND i.deleted_at IS NULL
 LEFT JOIN (SELECT inventory_id,SUM(status='available') available,SUM(status='reserved') reserved,SUM(status='sold') sold FROM stock_items GROUP BY inventory_id) s ON s.inventory_id=i.id
 GROUP BY ip.product_id
 ) b ON b.product_id=p.id AND p.delivery_mode='automatic'`

type scanner interface{ Scan(...any) error }

func scanProduct(row scanner) (Product, error) {
	var p Product
	err := row.Scan(&p.ID, &p.Name, &p.Description, &p.Details, &p.Price, &p.Currency, &p.Kind, &p.Active, &p.Hidden, &p.UpdatedAt, &p.Available, &p.Reserved, &p.Sold, &p.InventoryStrategy, &p.InventoryCount, &p.StockUnlimited, &p.DeliveryMode, &p.DeliveryNote, &p.BuyerNoteLabel, &p.BuyerNoteRequired, &p.MinQuantity, &p.MaxQuantity)
	return p, err
}
func (a *App) products(active bool) ([]Product, error) {
	q := productSelect + " WHERE p.deleted_at IS NULL"
	if active {
		// 隐藏仅控制商城列表，直接访问详情和下单仍按上架状态判断。
		q += " AND p.active=1 AND p.hidden=0"
	}
	q += " ORDER BY p.created_at DESC"
	rows, err := a.db.Query(q)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := []Product{}
	for rows.Next() {
		p, e := scanProduct(rows)
		if e != nil {
			return nil, e
		}
		if active {
			p.Details = "" // 商城卡片只需简介，详细内容在打开商品时读取。
			p.Reserved = 0
			p.Sold = 0
		}
		result = append(result, p)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, err
	}
	if active {
		for i := range result {
			if err = a.productOffer(context.Background(), &result[i]); err != nil {
				return nil, err
			}
		}
	}
	return result, nil
}

func (a *App) productOffer(ctx context.Context, p *Product) error {
	quote, err := quoteOrder(ctx, a.db, *p, 1, "", unixNow())
	if err != nil {
		return err
	}
	if quote.DiscountTotal > 0 {
		p.SalePrice = &quote.Total
		p.OfferName = quote.Discounts[0].Name
	}
	return nil
}
func (a *App) listProducts(w http.ResponseWriter, r *http.Request) error {
	ps, err := a.products(true)
	if err != nil {
		return err
	}
	reply(w, 200, ps)
	return nil
}
func (a *App) adminProducts(w http.ResponseWriter, r *http.Request) error {
	ps, err := a.products(false)
	if err != nil {
		return err
	}
	reply(w, 200, ps)
	return nil
}
func (a *App) productDetail(w http.ResponseWriter, r *http.Request) error {
	p, err := scanProduct(a.db.QueryRow(productSelect+" WHERE p.id=? AND p.active=1 AND p.deleted_at IS NULL", r.PathValue("id")))
	if err == sql.ErrNoRows {
		return &apiError{404, "商品不存在或已下架"}
	}
	if err != nil {
		return err
	}
	p.Reserved = 0
	p.Sold = 0
	if err = a.productOffer(r.Context(), &p); err != nil {
		return err
	}
	reply(w, 200, p)
	return nil
}

type productInput struct {
	Name              string  `json:"name"`
	Description       string  `json:"description"`
	Details           *string `json:"details"`
	Price             int64   `json:"price"`
	Currency          string  `json:"currency"`
	InventoryStrategy *string `json:"inventory_strategy"`
	Active            bool    `json:"active"`
	Hidden            *bool   `json:"hidden"`
	DeliveryMode      *string `json:"delivery_mode"`
	DeliveryNote      *string `json:"delivery_note"`
	BuyerNoteLabel    *string `json:"buyer_note_label"`
	BuyerNoteRequired *bool   `json:"buyer_note_required"`
	MinQuantity       *int    `json:"min_quantity"`
	MaxQuantity       *int    `json:"max_quantity"`
}

func (a *App) saveProduct(w http.ResponseWriter, r *http.Request) error {
	var input productInput
	if err := readJSON(w, r, &input); err != nil {
		return err
	}
	input.Name = strings.TrimSpace(input.Name)
	input.Currency = strings.ToUpper(input.Currency)
	if input.Name == "" || !utf8.ValidString(input.Name) || utf8.RuneCountInString(input.Name) > 120 || !utf8.ValidString(input.Description) || utf8.RuneCountInString(input.Description) > 4000 || input.Price <= 0 || input.Price > 9_000_000_000_000 {
		return bad("商品名称、价格或内容长度无效")
	}
	if _, err := currencyScale(input.Currency); err != nil {
		return bad(err.Error())
	}
	if input.Details != nil && (!utf8.ValidString(*input.Details) || utf8.RuneCountInString(*input.Details) > 65536) {
		return bad("商品详情不能超过 65536 字符")
	}
	tx, err := a.db.BeginTx(r.Context(), nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	id := r.PathValue("id")
	details := ""
	hidden := false
	policy := Product{DeliveryMode: "automatic", InventoryStrategy: "oldest", MinQuantity: 1, MaxQuantity: 100}
	if id == "" {
		id = newID()
	} else {
		if err := tx.QueryRowContext(r.Context(), "SELECT inventory_strategy,details,hidden,delivery_mode,delivery_note,buyer_note_label,buyer_note_required,min_quantity,max_quantity FROM products WHERE id=? AND deleted_at IS NULL", id).Scan(&policy.InventoryStrategy, &details, &hidden, &policy.DeliveryMode, &policy.DeliveryNote, &policy.BuyerNoteLabel, &policy.BuyerNoteRequired, &policy.MinQuantity, &policy.MaxQuantity); err == sql.ErrNoRows {
			return &apiError{404, "商品不存在"}
		} else if err != nil {
			return err
		}
	}
	if input.Details != nil {
		details = *input.Details
	}
	if input.Hidden != nil {
		hidden = *input.Hidden
	}
	if input.InventoryStrategy != nil {
		policy.InventoryStrategy = *input.InventoryStrategy
	}
	if input.DeliveryMode != nil {
		policy.DeliveryMode = *input.DeliveryMode
	}
	if input.DeliveryNote != nil {
		policy.DeliveryNote = strings.TrimSpace(*input.DeliveryNote)
	}
	if input.BuyerNoteLabel != nil {
		policy.BuyerNoteLabel = strings.TrimSpace(*input.BuyerNoteLabel)
	}
	if input.BuyerNoteRequired != nil {
		policy.BuyerNoteRequired = *input.BuyerNoteRequired
	}
	if input.MinQuantity != nil {
		policy.MinQuantity = *input.MinQuantity
	}
	if input.MaxQuantity != nil {
		policy.MaxQuantity = *input.MaxQuantity
	}
	if policy.DeliveryMode != "automatic" && policy.DeliveryMode != "manual" {
		return bad("交付方式无效")
	}
	if policy.InventoryStrategy != "random" && policy.InventoryStrategy != "oldest" && policy.InventoryStrategy != "newest" {
		return bad("库存分配策略无效")
	}
	if policy.MinQuantity < 1 || policy.MaxQuantity > 100 || policy.MinQuantity > policy.MaxQuantity {
		return bad("购买数量范围须在 1–100 之间，且下限不能大于上限")
	}
	if !utf8.ValidString(policy.DeliveryNote) || utf8.RuneCountInString(policy.DeliveryNote) > 4000 || !utf8.ValidString(policy.BuyerNoteLabel) || utf8.RuneCountInString(policy.BuyerNoteLabel) > 120 {
		return bad("交付说明或买家信息标题过长")
	}
	if policy.BuyerNoteRequired && policy.BuyerNoteLabel == "" {
		return bad("请先设置买家信息标题")
	}
	now := unixNow()
	result, err := tx.ExecContext(r.Context(), `INSERT INTO products(id,name,description,details,price,currency,inventory_strategy,active,hidden,created_at,updated_at,delivery_mode,delivery_note,buyer_note_label,buyer_note_required,min_quantity,max_quantity) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)
 ON CONFLICT(id) DO UPDATE SET name=excluded.name,description=excluded.description,details=excluded.details,price=excluded.price,currency=excluded.currency,inventory_strategy=excluded.inventory_strategy,active=excluded.active,hidden=excluded.hidden,updated_at=excluded.updated_at,delivery_mode=excluded.delivery_mode,delivery_note=excluded.delivery_note,buyer_note_label=excluded.buyer_note_label,buyer_note_required=excluded.buyer_note_required,min_quantity=excluded.min_quantity,max_quantity=excluded.max_quantity WHERE products.deleted_at IS NULL`, id, input.Name, input.Description, details, input.Price, input.Currency, policy.InventoryStrategy, input.Active, hidden, now, now, policy.DeliveryMode, policy.DeliveryNote, policy.BuyerNoteLabel, policy.BuyerNoteRequired, policy.MinQuantity, policy.MaxQuantity)
	if err != nil {
		return err
	}
	count, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if count == 0 {
		return &apiError{404, "商品不存在"}
	}
	if policy.DeliveryMode == "manual" {
		if _, err = tx.ExecContext(r.Context(), "DELETE FROM inventory_products WHERE product_id=?", id); err != nil {
			return err
		}
	}
	if err = tx.Commit(); err != nil {
		return err
	}
	audit("product_saved", id)
	p, err := scanProduct(a.db.QueryRow(productSelect+" WHERE p.id=?", id))
	if err != nil {
		return err
	}
	reply(w, 200, p)
	return nil
}

func (a *App) deleteProduct(w http.ResponseWriter, r *http.Request) error {
	// 保留订单外键、库存预留及交付密文，原订单继续核验付款和领取。
	result, err := a.db.ExecContext(r.Context(), "UPDATE products SET active=0,deleted_at=COALESCE(deleted_at,?),updated_at=? WHERE id=?", unixNow(), unixNow(), r.PathValue("id"))
	if err != nil {
		return err
	}
	count, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if count == 0 {
		return &apiError{404, "商品不存在"}
	}
	audit("product_deleted", r.PathValue("id"))
	reply(w, 200, map[string]bool{"ok": true})
	return nil
}
