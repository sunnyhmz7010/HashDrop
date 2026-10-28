package main

import (
	"context"
	"database/sql"
	"net/http"
	"strings"
	"unicode/utf8"
)

type Product struct {
	ID                string           `json:"id"`
	Name              string           `json:"name"`
	Description       string           `json:"description"`
	Details           string           `json:"details"`
	Price             int64            `json:"price"`
	SalePrice         *int64           `json:"sale_price,omitempty"`
	OfferName         string           `json:"offer_name,omitempty"`
	Currency          string           `json:"currency"`
	InventoryStrategy string           `json:"inventory_strategy"`
	InventoryCount    int              `json:"inventory_count"`
	StockUnlimited    bool             `json:"stock_unlimited"`
	DeliveryPlan      *ContentDocument `json:"delivery_plan,omitempty"`
	DeliveryNote      string           `json:"delivery_note"`
	MinQuantity       int              `json:"min_quantity"`
	MaxQuantity       int              `json:"max_quantity"`
	Active            bool             `json:"active"`
	Hidden            bool             `json:"hidden"`
	Available         int              `json:"available"`
	Reserved          int              `json:"reserved"`
	Sold              int              `json:"sold"`
	UpdatedAt         int64            `json:"updated_at"`
	planCipher        string
}

const productSelect = `SELECT p.id,p.name,p.description,p.details,p.price,p.currency,
 p.active,p.hidden,p.updated_at,p.inventory_strategy,p.delivery_plan,p.delivery_note,p.min_quantity,p.max_quantity FROM products p`

type scanner interface{ Scan(...any) error }

func scanProduct(row scanner) (Product, error) {
	var p Product
	err := row.Scan(&p.ID, &p.Name, &p.Description, &p.Details, &p.Price, &p.Currency, &p.Active, &p.Hidden, &p.UpdatedAt, &p.InventoryStrategy, &p.planCipher, &p.DeliveryNote, &p.MinQuantity, &p.MaxQuantity)
	return p, err
}

// 查询与下单共用同一份计划和容量计算；传入 tx 时不占用第二条连接。
func (a *App) hydrateProduct(ctx context.Context, q inventoryQuerier, p *Product) error {
	plain, err := a.vault.decrypt(p.planCipher, "product-plan:"+p.ID)
	if err != nil {
		return err
	}
	doc, err := decodeContent(plain)
	if err != nil {
		return err
	}
	p.DeliveryPlan = &doc
	catalog, err := loadInventoryCatalog(ctx, q, *p)
	if err != nil {
		return err
	}
	p.InventoryCount = len(catalog.pools)
	p.Reserved, p.Sold = 0, 0
	upper := 0
	for _, pool := range catalog.pools {
		p.Reserved += pool.reserved
		p.Sold += pool.sold
		upper += pool.available
	}
	p.StockUnlimited = true
	for _, need := range catalog.needs {
		unlimited := false
		for _, id := range need.pools {
			pool := catalog.pools[id]
			unlimited = unlimited || pool.reusable && pool.available > 0
		}
		p.StockUnlimited = p.StockUnlimited && unlimited
	}
	p.Available = 0
	if !p.StockUnlimited {
		// 共享池使逐项取 min 得到虚高库存；与实际分配使用相同的容量匹配。
		lo, hi := 0, upper
		for lo < hi {
			mid := lo + (hi-lo+1)/2
			if _, ok := matchInventory(catalog, mid); ok {
				lo = mid
			} else {
				hi = mid - 1
			}
		}
		p.Available = lo
	}
	return nil
}

func (a *App) products(active bool) ([]Product, error) {
	q := productSelect + " WHERE p.deleted_at IS NULL"
	if active {
		q += " AND p.active=1 AND p.hidden=0"
	}
	rows, err := a.db.Query(q + " ORDER BY p.created_at DESC,p.id")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := []Product{}
	for rows.Next() {
		p, err := scanProduct(rows)
		if err != nil {
			return nil, err
		}
		result = append(result, p)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, err
	}
	for i := range result {
		p := &result[i]
		if err = a.hydrateProduct(context.Background(), a.db, p); err != nil {
			return nil, err
		}
		if active {
			p.Details, p.DeliveryPlan, p.Reserved, p.Sold = "", nil, 0, 0
			if err = a.productOffer(context.Background(), p); err != nil {
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
	p, err := scanProduct(a.db.QueryRowContext(r.Context(), productSelect+" WHERE p.id=? AND p.active=1 AND p.deleted_at IS NULL", r.PathValue("id")))
	if err == sql.ErrNoRows {
		return &apiError{404, "商品不存在或已下架"}
	}
	if err != nil {
		return err
	}
	if err = a.hydrateProduct(r.Context(), a.db, &p); err != nil {
		return err
	}
	p.DeliveryPlan, p.Reserved, p.Sold = nil, 0, 0
	if err = a.productOffer(r.Context(), &p); err != nil {
		return err
	}
	reply(w, 200, p)
	return nil
}

type productInput struct {
	Name              string           `json:"name"`
	Description       string           `json:"description"`
	Details           *string          `json:"details"`
	Price             int64            `json:"price"`
	Currency          string           `json:"currency"`
	InventoryStrategy *string          `json:"inventory_strategy"`
	Active            bool             `json:"active"`
	Hidden            *bool            `json:"hidden"`
	DeliveryPlan      *ContentDocument `json:"delivery_plan"`
	DeliveryNote      *string          `json:"delivery_note"`
	MinQuantity       *int             `json:"min_quantity"`
	MaxQuantity       *int             `json:"max_quantity"`
}

func (a *App) saveProduct(w http.ResponseWriter, r *http.Request) error {
	var input productInput
	if err := readJSON(w, r, &input); err != nil {
		return err
	}
	input.Name, input.Currency = strings.TrimSpace(input.Name), strings.ToUpper(input.Currency)
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
	p := Product{InventoryStrategy: "oldest", MinQuantity: 1, MaxQuantity: 100}
	if id == "" {
		id = newID()
	} else {
		p, err = scanProduct(tx.QueryRowContext(r.Context(), productSelect+" WHERE p.id=? AND p.deleted_at IS NULL", id))
		if err == sql.ErrNoRows {
			return &apiError{404, "商品不存在"}
		}
		if err != nil {
			return err
		}
		if input.DeliveryPlan == nil {
			plain, err := a.vault.decrypt(p.planCipher, "product-plan:"+id)
			if err != nil {
				return err
			}
			doc, err := decodeContent(plain)
			if err != nil {
				return err
			}
			p.DeliveryPlan = &doc
		}
	}
	if input.Details != nil {
		p.Details = *input.Details
	}
	if input.Hidden != nil {
		p.Hidden = *input.Hidden
	}
	if input.InventoryStrategy != nil {
		p.InventoryStrategy = *input.InventoryStrategy
	}
	if input.DeliveryPlan != nil {
		p.DeliveryPlan = input.DeliveryPlan
	}
	if input.DeliveryNote != nil {
		p.DeliveryNote = strings.TrimSpace(*input.DeliveryNote)
	}
	if input.MinQuantity != nil {
		p.MinQuantity = *input.MinQuantity
	}
	if input.MaxQuantity != nil {
		p.MaxQuantity = *input.MaxQuantity
	}
	if p.InventoryStrategy != "random" && p.InventoryStrategy != "oldest" && p.InventoryStrategy != "newest" {
		return bad("库存分配策略无效")
	}
	if p.MinQuantity < 1 || p.MaxQuantity > 100 || p.MinQuantity > p.MaxQuantity {
		return bad("购买数量范围须在 1–100 之间，且下限不能大于上限")
	}
	if !utf8.ValidString(p.DeliveryNote) || utf8.RuneCountInString(p.DeliveryNote) > 4000 {
		return bad("交付说明不能超过 4000 字符")
	}
	if p.DeliveryPlan == nil {
		return bad("请设置商品交付内容")
	}
	if err = validateContent(*p.DeliveryPlan, true); err != nil {
		return err
	}
	plain, err := encodeContent(*p.DeliveryPlan)
	if err != nil {
		return err
	}
	cipher, err := a.vault.encrypt(plain, "product-plan:"+id)
	if err != nil {
		return err
	}
	now := unixNow()
	if _, err = tx.ExecContext(r.Context(), `INSERT INTO products(id,name,description,details,price,currency,inventory_strategy,active,hidden,created_at,updated_at,delivery_plan,delivery_note,min_quantity,max_quantity) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)
 ON CONFLICT(id) DO UPDATE SET name=excluded.name,description=excluded.description,details=excluded.details,price=excluded.price,currency=excluded.currency,inventory_strategy=excluded.inventory_strategy,active=excluded.active,hidden=excluded.hidden,updated_at=excluded.updated_at,delivery_plan=excluded.delivery_plan,delivery_note=excluded.delivery_note,min_quantity=excluded.min_quantity,max_quantity=excluded.max_quantity`, id, input.Name, input.Description, p.Details, input.Price, input.Currency, p.InventoryStrategy, input.Active, p.Hidden, now, now, cipher, p.DeliveryNote, p.MinQuantity, p.MaxQuantity); err != nil {
		return err
	}
	// 移除已删除取货项的绑定；历史订单仍使用原分配与快照。
	query, args := "DELETE FROM inventory_products WHERE product_id=?", []any{id}
	slots := []string{}
	for _, b := range p.DeliveryPlan.Blocks {
		if b.Type == "stock" {
			slots = append(slots, b.ID)
			args = append(args, b.ID)
		}
	}
	if len(slots) > 0 {
		query += " AND slot_id NOT IN (" + strings.TrimSuffix(strings.Repeat("?,", len(slots)), ",") + ")"
	}
	if _, err = tx.ExecContext(r.Context(), query, args...); err != nil {
		return err
	}
	if err = tx.Commit(); err != nil {
		return err
	}
	audit("product_saved", id)
	p, err = scanProduct(a.db.QueryRowContext(r.Context(), productSelect+" WHERE p.id=?", id))
	if err != nil {
		return err
	}
	if err = a.hydrateProduct(r.Context(), a.db, &p); err != nil {
		return err
	}
	reply(w, 200, p)
	return nil
}

func (a *App) deleteProduct(w http.ResponseWriter, r *http.Request) error {
	tx, err := a.db.BeginTx(r.Context(), nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	result, err := tx.ExecContext(r.Context(), "UPDATE products SET active=0,deleted_at=COALESCE(deleted_at,?),updated_at=? WHERE id=?", unixNow(), unixNow(), r.PathValue("id"))
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
	audit("product_deleted", r.PathValue("id"))
	reply(w, 200, map[string]bool{"ok": true})
	return nil
}
