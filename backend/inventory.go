package main

import (
	"database/sql"
	"net/http"
	"strings"
)

type Inventory struct {
	ID           string `json:"id"`
	Name         string `json:"name"`
	Kind         string `json:"kind"`
	HasContent   bool   `json:"has_content"`
	Available    int    `json:"available"`
	Reserved     int    `json:"reserved"`
	Sold         int    `json:"sold"`
	ProductCount int    `json:"product_count"`
	UpdatedAt    int64  `json:"updated_at"`
}

const inventorySelect = `SELECT i.id,i.name,i.kind,i.content<>'',i.updated_at,
 (SELECT COUNT(*) FROM stock_items s WHERE s.inventory_id=i.id AND s.status='available'),
 (SELECT COUNT(*) FROM stock_items s WHERE s.inventory_id=i.id AND s.status='reserved'),
 (SELECT COUNT(*) FROM stock_items s WHERE s.inventory_id=i.id AND s.status='sold'),
 (SELECT COUNT(*) FROM products p WHERE p.inventory_id=i.id AND p.deleted_at IS NULL)
 FROM inventories i`

func validDeliveryKind(kind string) bool {
	return kind == "card" || kind == "account" || kind == "static"
}

func scanInventory(row scanner) (Inventory, error) {
	var i Inventory
	err := row.Scan(&i.ID, &i.Name, &i.Kind, &i.HasContent, &i.UpdatedAt, &i.Available, &i.Reserved, &i.Sold, &i.ProductCount)
	return i, err
}

func (a *App) adminInventories(w http.ResponseWriter, r *http.Request) error {
	rows, err := a.db.QueryContext(r.Context(), inventorySelect+" WHERE i.deleted_at IS NULL ORDER BY i.created_at DESC,i.id")
	if err != nil {
		return err
	}
	defer rows.Close()
	items := []Inventory{}
	for rows.Next() {
		i, err := scanInventory(rows)
		if err != nil {
			return err
		}
		items = append(items, i)
	}
	if err = rows.Err(); err != nil {
		return err
	}
	reply(w, 200, items)
	return nil
}

func (a *App) saveInventory(w http.ResponseWriter, r *http.Request) error {
	var input struct {
		Name          string `json:"name"`
		Kind          string `json:"kind"`
		StaticContent string `json:"static_content"`
	}
	if err := readJSON(w, r, &input); err != nil {
		return err
	}
	input.Name = strings.TrimSpace(input.Name)
	if input.Name == "" || len(input.Name) > 120 || !validDeliveryKind(input.Kind) || len(input.StaticContent) > 65536 {
		return bad("库存池名称、交付类型或内容长度无效")
	}
	if input.Kind != "static" && input.StaticContent != "" {
		return bad("只有固定文本库存池可以设置固定内容")
	}
	tx, err := a.db.BeginTx(r.Context(), nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	id := r.PathValue("id")
	content, aad := "", ""
	if id == "" {
		id = newID()
	} else {
		var kind string
		if err = tx.QueryRowContext(r.Context(), "SELECT kind,content,content_aad FROM inventories WHERE id=? AND deleted_at IS NULL", id).Scan(&kind, &content, &aad); err == sql.ErrNoRows {
			return &apiError{404, "库存池不存在或已删除"}
		} else if err != nil {
			return err
		}
		if kind != input.Kind {
			return bad("已有库存池不能改变交付类型，请新建库存池后关联商品")
		}
	}
	if input.Kind == "static" {
		if input.StaticContent != "" {
			aad = "inventory:" + id
			content, err = a.vault.encrypt(input.StaticContent, aad)
			if err != nil {
				return err
			}
		}
		if content == "" {
			return bad("固定内容不能为空")
		}
	}
	now := unixNow()
	if _, err = tx.ExecContext(r.Context(), `INSERT INTO inventories(id,name,kind,content,content_aad,created_at,updated_at) VALUES(?,?,?,?,?,?,?)
 ON CONFLICT(id) DO UPDATE SET name=excluded.name,content=excluded.content,content_aad=excluded.content_aad,updated_at=excluded.updated_at`, id, input.Name, input.Kind, content, aad, now, now); err != nil {
		return err
	}
	if err = tx.Commit(); err != nil {
		return err
	}
	item, err := scanInventory(a.db.QueryRowContext(r.Context(), inventorySelect+" WHERE i.id=?", id))
	if err != nil {
		return err
	}
	audit("inventory_saved", id)
	reply(w, 200, item)
	return nil
}

func (a *App) deleteInventory(w http.ResponseWriter, r *http.Request) error {
	tx, err := a.db.BeginTx(r.Context(), nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	id := r.PathValue("id")
	var exists string
	if err = tx.QueryRowContext(r.Context(), "SELECT id FROM inventories WHERE id=?", id).Scan(&exists); err == sql.ErrNoRows {
		return &apiError{404, "库存池不存在或已删除"}
	} else if err != nil {
		return err
	}
	var linked, reserved int
	if err = tx.QueryRowContext(r.Context(), "SELECT COUNT(*) FROM products WHERE inventory_id=? AND deleted_at IS NULL", id).Scan(&linked); err != nil {
		return err
	}
	if linked > 0 {
		return &apiError{409, "库存池仍有关联商品，请先解除关联"}
	}
	if err = tx.QueryRowContext(r.Context(), "SELECT COUNT(*) FROM stock_items WHERE inventory_id=? AND status='reserved'", id).Scan(&reserved); err != nil {
		return err
	}
	if reserved > 0 {
		return &apiError{409, "库存池仍有订单预留，暂不能删除"}
	}
	// 逻辑删除保留售出记录和历史订单的外键，商品删除也不会删除库存池。
	if _, err = tx.ExecContext(r.Context(), "UPDATE inventories SET deleted_at=COALESCE(deleted_at,?),updated_at=? WHERE id=?", unixNow(), unixNow(), id); err != nil {
		return err
	}
	if err = tx.Commit(); err != nil {
		return err
	}
	audit("inventory_deleted", id)
	reply(w, 200, map[string]bool{"ok": true})
	return nil
}

func (a *App) importStock(w http.ResponseWriter, r *http.Request) error {
	var input struct {
		Content string `json:"content"`
	}
	if err := readJSON(w, r, &input); err != nil {
		return err
	}
	id := r.PathValue("id")
	lines := strings.Split(strings.ReplaceAll(input.Content, "\r\n", "\n"), "\n")
	if len(lines) > 5000 {
		return bad("一次最多导入 5000 行")
	}
	tx, err := a.db.BeginTx(r.Context(), nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var kind string
	if err := tx.QueryRowContext(r.Context(), "SELECT kind FROM inventories WHERE id=? AND deleted_at IS NULL", id).Scan(&kind); err == sql.ErrNoRows {
		return &apiError{404, "库存池不存在或已删除"}
	} else if err != nil {
		return err
	}
	if kind != "card" && kind != "account" {
		return bad("固定文本库存池不使用逐条库存")
	}
	added, duplicates := 0, 0
	for _, line := range lines {
		content := strings.TrimSpace(line)
		if content == "" {
			continue
		}
		if len(content) > 16384 {
			return bad("单份库存最多 16 KB")
		}
		stockID := newID()
		cipher, err := a.vault.encrypt(content, "stock:"+stockID)
		if err != nil {
			return err
		}
		result, err := tx.ExecContext(r.Context(), "INSERT INTO stock_items(id,inventory_id,content,fingerprint,created_at) VALUES(?,?,?,?,?) ON CONFLICT(inventory_id,fingerprint) DO NOTHING", stockID, id, cipher, a.vault.fingerprint(id, content), unixNow())
		if err != nil {
			return err
		}
		n, err := result.RowsAffected()
		if err != nil {
			return err
		}
		if n == 1 {
			added++
		} else {
			duplicates++
		}
	}
	if added+duplicates == 0 {
		return bad("请填写库存内容，每行一份")
	}
	if err = tx.Commit(); err != nil {
		return err
	}
	audit("stock_imported", id)
	reply(w, 200, map[string]int{"added": added, "duplicates": duplicates})
	return nil
}
func (a *App) listStock(w http.ResponseWriter, r *http.Request) error {
	var inventory string
	if err := a.db.QueryRowContext(r.Context(), "SELECT id FROM inventories WHERE id=? AND deleted_at IS NULL", r.PathValue("id")).Scan(&inventory); err == sql.ErrNoRows {
		return &apiError{404, "库存池不存在或已删除"}
	} else if err != nil {
		return err
	}
	rows, err := a.db.QueryContext(r.Context(), "SELECT id,status,COALESCE(order_id,''),created_at FROM stock_items WHERE inventory_id=? ORDER BY created_at DESC,id LIMIT 200", inventory)
	if err != nil {
		return err
	}
	defer rows.Close()
	items := []map[string]any{}
	for rows.Next() {
		var id, status, order string
		var created int64
		if err = rows.Scan(&id, &status, &order, &created); err != nil {
			return err
		}
		items = append(items, map[string]any{"id": id, "status": status, "order_id": order, "created_at": created})
	}
	if err = rows.Err(); err != nil {
		return err
	}
	reply(w, 200, items)
	return nil
}
func (a *App) deleteStock(w http.ResponseWriter, r *http.Request) error {
	result, err := a.db.ExecContext(r.Context(), "DELETE FROM stock_items WHERE id=? AND inventory_id=? AND status='available' AND inventory_id IN (SELECT id FROM inventories WHERE deleted_at IS NULL)", r.PathValue("stock"), r.PathValue("id"))
	if err != nil {
		return err
	}
	n, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if n != 1 {
		return bad("只能删除尚未预留的库存")
	}
	audit("stock_deleted", r.PathValue("stock"))
	reply(w, 200, map[string]bool{"ok": true})
	return nil
}
