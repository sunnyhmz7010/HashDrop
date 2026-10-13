package main

import (
	"database/sql"
	"encoding/json"
	"net/http"
	"strings"
)

type Product struct {
	ID          string          `json:"id"`
	Name        string          `json:"name"`
	Description string          `json:"description"`
	Price       int64           `json:"price"`
	Currency    string          `json:"currency"`
	Kind        string          `json:"kind"`
	Active      bool            `json:"active"`
	Available   int             `json:"available"`
	Reserved    int             `json:"reserved"`
	Sold        int             `json:"sold"`
	Config      json.RawMessage `json:"config"`
	Content     string          `json:"-"`
	UpdatedAt   int64           `json:"updated_at"`
}

const productSelect = `SELECT p.id,p.name,p.description,p.price,p.currency,p.kind,p.active,p.config,p.content,p.updated_at,
 (SELECT COUNT(*) FROM stock_items s WHERE s.product_id=p.id AND s.status='available'),
 (SELECT COUNT(*) FROM stock_items s WHERE s.product_id=p.id AND s.status='reserved'),
 (SELECT COUNT(*) FROM stock_items s WHERE s.product_id=p.id AND s.status='sold') FROM products p`

type scanner interface{ Scan(...any) error }

func scanProduct(row scanner) (Product, error) {
	var p Product
	var config string
	err := row.Scan(&p.ID, &p.Name, &p.Description, &p.Price, &p.Currency, &p.Kind, &p.Active, &config, &p.Content, &p.UpdatedAt, &p.Available, &p.Reserved, &p.Sold)
	p.Config = json.RawMessage(config)
	return p, err
}
func (a *App) products(active bool) ([]Product, error) {
	q := productSelect
	if active {
		q += " WHERE p.active=1"
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
			if p.Kind == "api" {
				var cfg AdapterConfig
				if err = json.Unmarshal(p.Config, &cfg); err != nil {
					return nil, err
				}
				if _, exists := a.adapters[cfg.Adapter]; !exists {
					continue
				}
			}
			p.Config = json.RawMessage(`{}`)
			p.Reserved = 0
			p.Sold = 0
		}
		result = append(result, p)
	}
	return result, rows.Err()
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
	p, err := scanProduct(a.db.QueryRow(productSelect+" WHERE p.id=? AND p.active=1", r.PathValue("id")))
	if err == sql.ErrNoRows {
		return &apiError{404, "商品不存在或已下架"}
	}
	if err != nil {
		return err
	}
	p.Config = json.RawMessage(`{}`)
	p.Reserved = 0
	p.Sold = 0
	reply(w, 200, p)
	return nil
}

type productInput struct {
	Name          string          `json:"name"`
	Description   string          `json:"description"`
	Price         int64           `json:"price"`
	Currency      string          `json:"currency"`
	Kind          string          `json:"kind"`
	Active        bool            `json:"active"`
	StaticContent string          `json:"static_content"`
	Config        json.RawMessage `json:"config"`
}

func (a *App) saveProduct(w http.ResponseWriter, r *http.Request) error {
	var input productInput
	if err := readJSON(w, r, &input); err != nil {
		return err
	}
	input.Name = strings.TrimSpace(input.Name)
	input.Currency = strings.ToUpper(input.Currency)
	if len(input.Name) == 0 || len(input.Name) > 120 || len(input.Description) > 4000 || input.Price <= 0 || input.Price > 9_000_000_000_000 || len(input.StaticContent) > 65536 {
		return bad("商品名称、价格或内容长度无效")
	}
	if _, err := currencyScale(input.Currency); err != nil {
		return bad(err.Error())
	}
	switch input.Kind {
	case "card", "account", "static", "api":
	default:
		return bad("商品类型无效")
	}
	if len(input.Config) == 0 {
		input.Config = json.RawMessage(`{}`)
	}
	if input.Kind == "api" {
		var cfg AdapterConfig
		if err := json.Unmarshal(input.Config, &cfg); err != nil {
			return bad("API 配置无效")
		}
		adapter, exists := a.adapters[cfg.Adapter]
		if input.Active && !exists {
			return bad("该 API 适配器尚未部署，不能上架")
		}
		if exists {
			if err := adapter.Validate(cfg.Parameters); err != nil {
				return bad("API 适配器参数无效")
			}
		}
	} else {
		input.Config = json.RawMessage(`{}`)
	}
	id := r.PathValue("id")
	content := ""
	if id == "" {
		id = newID()
	} else {
		var kind string
		if err := a.db.QueryRow("SELECT kind,content FROM products WHERE id=?", id).Scan(&kind, &content); err == sql.ErrNoRows {
			return &apiError{404, "商品不存在"}
		} else if err != nil {
			return err
		}
		if kind != input.Kind {
			return bad("已有商品不能改变交付类型，请新建商品")
		}
	}
	if input.Kind == "static" {
		if input.StaticContent != "" {
			var err error
			content, err = a.vault.encrypt(input.StaticContent, "product:"+id)
			if err != nil {
				return err
			}
		}
		if content == "" {
			return bad("固定内容不能为空")
		}
	}
	now := unixNow()
	_, err := a.db.Exec(`INSERT INTO products(id,name,description,price,currency,kind,active,config,content,created_at,updated_at) VALUES(?,?,?,?,?,?,?,?,?,?,?)
 ON CONFLICT(id) DO UPDATE SET name=excluded.name,description=excluded.description,price=excluded.price,currency=excluded.currency,active=excluded.active,config=excluded.config,content=excluded.content,updated_at=excluded.updated_at`, id, input.Name, input.Description, input.Price, input.Currency, input.Kind, input.Active, string(input.Config), content, now, now)
	if err != nil {
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
