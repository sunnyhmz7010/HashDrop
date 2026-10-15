package main

import (
	"context"
	"database/sql"
	"net/http"
	"strings"
	"unicode/utf8"
)

type Product struct {
	ID            string `json:"id"`
	Name          string `json:"name"`
	Description   string `json:"description"`
	Details       string `json:"details"`
	Price         int64  `json:"price"`
	SalePrice     *int64 `json:"sale_price,omitempty"`
	OfferName     string `json:"offer_name,omitempty"`
	Currency      string `json:"currency"`
	InventoryID   string `json:"inventory_id,omitempty"`
	InventoryName string `json:"inventory_name,omitempty"`
	// Kind 来自关联库存池；商品本身不再保存交付类型和内容。
	Kind      string `json:"kind"`
	Active    bool   `json:"active"`
	Hidden    bool   `json:"hidden"`
	Available int    `json:"available"`
	Reserved  int    `json:"reserved"`
	Sold      int    `json:"sold"`
	Content   string `json:"-"`
	UpdatedAt int64  `json:"updated_at"`
}

const productSelect = `SELECT p.id,p.name,p.description,p.details,p.price,p.currency,COALESCE(i.kind,''),p.active,p.hidden,COALESCE(i.content,''),p.updated_at,
 (SELECT COUNT(*) FROM stock_items s WHERE s.inventory_id=i.id AND s.status='available'),
 (SELECT COUNT(*) FROM stock_items s WHERE s.inventory_id=i.id AND s.status='reserved'),
 (SELECT COUNT(*) FROM stock_items s WHERE s.inventory_id=i.id AND s.status='sold'),
 COALESCE(i.id,''),COALESCE(i.name,'')
 FROM products p LEFT JOIN inventories i ON i.id=p.inventory_id AND i.deleted_at IS NULL`

type scanner interface{ Scan(...any) error }

func scanProduct(row scanner) (Product, error) {
	var p Product
	err := row.Scan(&p.ID, &p.Name, &p.Description, &p.Details, &p.Price, &p.Currency, &p.Kind, &p.Active, &p.Hidden, &p.Content, &p.UpdatedAt, &p.Available, &p.Reserved, &p.Sold, &p.InventoryID, &p.InventoryName)
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
			p.InventoryID, p.InventoryName = "", ""
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
	p.InventoryID, p.InventoryName = "", ""
	if err = a.productOffer(r.Context(), &p); err != nil {
		return err
	}
	reply(w, 200, p)
	return nil
}

type productInput struct {
	Name        string  `json:"name"`
	Description string  `json:"description"`
	Details     *string `json:"details"`
	Price       int64   `json:"price"`
	Currency    string  `json:"currency"`
	InventoryID *string `json:"inventory_id"`
	Active      bool    `json:"active"`
	Hidden      *bool   `json:"hidden"`
}

func (a *App) saveProduct(w http.ResponseWriter, r *http.Request) error {
	var input productInput
	if err := readJSON(w, r, &input); err != nil {
		return err
	}
	input.Name = strings.TrimSpace(input.Name)
	input.Currency = strings.ToUpper(input.Currency)
	if len(input.Name) == 0 || len(input.Name) > 120 || len(input.Description) > 4000 || input.Price <= 0 || input.Price > 9_000_000_000_000 {
		return bad("商品名称、价格或内容长度无效")
	}
	if _, err := currencyScale(input.Currency); err != nil {
		return bad(err.Error())
	}
	if input.Details != nil && utf8.RuneCountInString(*input.Details) > 65536 {
		return bad("商品详情不能超过 65536 字符")
	}
	tx, err := a.db.BeginTx(r.Context(), nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	id := r.PathValue("id")
	inventory := ""
	details := ""
	hidden := false
	if id == "" {
		id = newID()
	} else {
		if err := tx.QueryRowContext(r.Context(), "SELECT COALESCE(inventory_id,''),details,hidden FROM products WHERE id=? AND deleted_at IS NULL", id).Scan(&inventory, &details, &hidden); err == sql.ErrNoRows {
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
	if input.InventoryID != nil {
		inventory = strings.TrimSpace(*input.InventoryID)
	}
	if inventory == "" && input.Active {
		return bad("上架前请选择库存池")
	}
	if inventory != "" {
		var kind, content string
		if err := tx.QueryRowContext(r.Context(), "SELECT kind,content FROM inventories WHERE id=? AND deleted_at IS NULL", inventory).Scan(&kind, &content); err == sql.ErrNoRows {
			return bad("库存池不存在或已删除")
		} else if err != nil {
			return err
		}
		if input.Active && kind == "static" && content == "" {
			return bad("固定内容不能为空")
		}
	}
	now := unixNow()
	var inventoryID any
	if inventory != "" {
		inventoryID = inventory
	}
	result, err := tx.ExecContext(r.Context(), `INSERT INTO products(id,name,description,details,price,currency,inventory_id,active,hidden,created_at,updated_at) VALUES(?,?,?,?,?,?,?,?,?,?,?)
 ON CONFLICT(id) DO UPDATE SET name=excluded.name,description=excluded.description,details=excluded.details,price=excluded.price,currency=excluded.currency,inventory_id=excluded.inventory_id,active=excluded.active,hidden=excluded.hidden,updated_at=excluded.updated_at WHERE products.deleted_at IS NULL`, id, input.Name, input.Description, details, input.Price, input.Currency, inventoryID, input.Active, hidden, now, now)
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
