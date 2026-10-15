package main

import (
	"database/sql"
	"net/http"
	"strings"
)

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
	if err := tx.QueryRowContext(r.Context(), "SELECT kind FROM products WHERE id=? AND deleted_at IS NULL", id).Scan(&kind); err == sql.ErrNoRows {
		return &apiError{404, "商品不存在"}
	} else if err != nil {
		return err
	}
	if kind != "card" && kind != "account" {
		return bad("该商品不使用库存")
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
		result, err := tx.Exec("INSERT INTO stock_items(id,product_id,content,fingerprint,created_at) VALUES(?,?,?,?,?) ON CONFLICT(product_id,fingerprint) DO NOTHING", stockID, id, cipher, a.vault.fingerprint(id, content), unixNow())
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
	var product string
	if err := a.db.QueryRowContext(r.Context(), "SELECT id FROM products WHERE id=? AND deleted_at IS NULL", r.PathValue("id")).Scan(&product); err == sql.ErrNoRows {
		return &apiError{404, "商品不存在"}
	} else if err != nil {
		return err
	}
	rows, err := a.db.Query("SELECT id,status,COALESCE(order_id,''),created_at FROM stock_items WHERE product_id=? ORDER BY created_at DESC,id LIMIT 200", r.PathValue("id"))
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
	result, err := a.db.Exec("DELETE FROM stock_items WHERE id=? AND product_id=? AND status='available' AND product_id IN (SELECT id FROM products WHERE deleted_at IS NULL)", r.PathValue("stock"), r.PathValue("id"))
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
