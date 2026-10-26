package main

import (
	"database/sql"
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"unicode/utf8"
)

type InventoryBinding struct {
	ProductID string `json:"product_id"`
	SlotID    string `json:"slot_id"`
}
type Inventory struct {
	ID           string             `json:"id"`
	Name         string             `json:"name"`
	Reusable     bool               `json:"reusable"`
	Available    int                `json:"available"`
	Reserved     int                `json:"reserved"`
	Sold         int                `json:"sold"`
	ProductCount int                `json:"product_count"`
	Bindings     []InventoryBinding `json:"bindings"`
	UpdatedAt    int64              `json:"updated_at"`
}

const inventorySelect = `SELECT i.id,i.name,i.reusable,i.updated_at,
 (SELECT COUNT(*) FROM stock_items s WHERE s.inventory_id=i.id AND s.status='available' AND s.deleted_at IS NULL),
 (SELECT COUNT(*) FROM stock_items s WHERE s.inventory_id=i.id AND s.status='reserved' AND s.deleted_at IS NULL),
 (SELECT COUNT(*) FROM stock_items s WHERE s.inventory_id=i.id AND s.status='sold' AND s.deleted_at IS NULL),
 (SELECT json_group_array(json_object('product_id',ip.product_id,'slot_id',ip.slot_id)) FROM inventory_products ip JOIN products p ON p.id=ip.product_id WHERE ip.inventory_id=i.id AND p.deleted_at IS NULL)
 FROM inventories i`

func scanInventory(row scanner) (Inventory, error) {
	var item Inventory
	var bindings string
	err := row.Scan(&item.ID, &item.Name, &item.Reusable, &item.UpdatedAt, &item.Available, &item.Reserved, &item.Sold, &bindings)
	if err != nil {
		return item, err
	}
	if err = json.Unmarshal([]byte(bindings), &item.Bindings); err != nil {
		return item, err
	}
	seen := map[string]bool{}
	for _, binding := range item.Bindings {
		seen[binding.ProductID] = true
	}
	item.ProductCount = len(seen)
	return item, nil
}

func (a *App) adminInventories(w http.ResponseWriter, r *http.Request) error {
	rows, err := a.db.QueryContext(r.Context(), inventorySelect+" WHERE i.deleted_at IS NULL ORDER BY i.created_at DESC,i.id")
	if err != nil {
		return err
	}
	defer rows.Close()
	items := []Inventory{}
	for rows.Next() {
		item, err := scanInventory(rows)
		if err != nil {
			return err
		}
		items = append(items, item)
	}
	if err = rows.Err(); err != nil {
		return err
	}
	reply(w, 200, items)
	return nil
}

func (a *App) saveInventory(w http.ResponseWriter, r *http.Request) error {
	var input struct {
		Name     string              `json:"name"`
		Reusable *bool               `json:"reusable"`
		Bindings *[]InventoryBinding `json:"bindings"`
	}
	if err := readJSON(w, r, &input); err != nil {
		return err
	}
	input.Name = strings.TrimSpace(input.Name)
	if input.Name == "" || !utf8.ValidString(input.Name) || utf8.RuneCountInString(input.Name) > 120 {
		return bad("库存池名称须为 1–120 字符")
	}
	if input.Bindings != nil && len(*input.Bindings) > 1000 {
		return bad("一个库存池最多绑定 1000 个取货项")
	}
	tx, err := a.db.BeginTx(r.Context(), nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	id, reusable := r.PathValue("id"), false
	if id == "" {
		id = newID()
	} else {
		err = tx.QueryRowContext(r.Context(), "SELECT reusable FROM inventories WHERE id=? AND deleted_at IS NULL", id).Scan(&reusable)
		if err == sql.ErrNoRows {
			return &apiError{404, "库存池不存在或已删除"}
		}
		if err != nil {
			return err
		}
	}
	if input.Reusable != nil {
		reusable = *input.Reusable
	}
	now := unixNow()
	if _, err = tx.ExecContext(r.Context(), `INSERT INTO inventories(id,name,reusable,created_at,updated_at) VALUES(?,?,?,?,?)
 ON CONFLICT(id) DO UPDATE SET name=excluded.name,reusable=excluded.reusable,updated_at=excluded.updated_at`, id, input.Name, reusable, now, now); err != nil {
		return err
	}
	if input.Bindings != nil {
		wanted := map[InventoryBinding]bool{}
		for _, binding := range *input.Bindings {
			if wanted[binding] {
				continue
			}
			var cipher string
			err = tx.QueryRowContext(r.Context(), "SELECT delivery_plan FROM products WHERE id=? AND deleted_at IS NULL", binding.ProductID).Scan(&cipher)
			if err == sql.ErrNoRows {
				return bad("关联商品不存在或已删除")
			}
			if err != nil {
				return err
			}
			plain, err := a.vault.decrypt(cipher, "product-plan:"+binding.ProductID)
			if err != nil {
				return err
			}
			doc, err := decodeContent(plain)
			if err != nil {
				return err
			}
			valid := false
			for _, block := range doc.Blocks {
				if block.Type == "stock" && block.ID == binding.SlotID {
					valid = true
					break
				}
			}
			if !valid {
				return bad("关联的商品取货项不存在，请刷新商品")
			}
			wanted[binding] = true
		}
		rows, err := tx.QueryContext(r.Context(), "SELECT product_id,slot_id FROM inventory_products WHERE inventory_id=?", id)
		if err != nil {
			return err
		}
		remove := []InventoryBinding{}
		for rows.Next() {
			var binding InventoryBinding
			if err = rows.Scan(&binding.ProductID, &binding.SlotID); err != nil {
				rows.Close()
				return err
			}
			if !wanted[binding] {
				remove = append(remove, binding)
			}
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return err
		}
		for _, binding := range remove {
			if _, err = tx.ExecContext(r.Context(), "DELETE FROM inventory_products WHERE inventory_id=? AND product_id=? AND slot_id=?", id, binding.ProductID, binding.SlotID); err != nil {
				return err
			}
		}
		// 按输入顺序新增，只删除差异；普通保存保持原绑定先后顺序。
		for _, binding := range *input.Bindings {
			if _, err = tx.ExecContext(r.Context(), "INSERT INTO inventory_products(inventory_id,product_id,slot_id,created_at) VALUES(?,?,?,?) ON CONFLICT(inventory_id,product_id,slot_id) DO NOTHING", id, binding.ProductID, binding.SlotID, now); err != nil {
				return err
			}
		}
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
	if err = tx.QueryRowContext(r.Context(), "SELECT COUNT(*) FROM inventory_products ip JOIN products p ON p.id=ip.product_id WHERE ip.inventory_id=? AND p.deleted_at IS NULL", id).Scan(&linked); err != nil {
		return err
	}
	if linked > 0 {
		return &apiError{409, "库存池仍有关联商品，请先解除关联"}
	}
	if err = tx.QueryRowContext(r.Context(), "SELECT COUNT(*) FROM delivery_allocations WHERE inventory_id=? AND status='reserved'", id).Scan(&reserved); err != nil {
		return err
	}
	if reserved > 0 {
		return &apiError{409, "库存池仍有订单预留，暂不能删除"}
	}
	// 逻辑删除保留历史分配外键；不会改变已售出结果或历史附件引用。
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
		Items []ContentDocument `json:"items"`
	}
	if err := readJSON(w, r, &input); err != nil {
		return err
	}
	if len(input.Items) < 1 || len(input.Items) > 5000 {
		return bad("一次须导入 1–5000 份库存内容")
	}
	tx, err := a.db.BeginTx(r.Context(), nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	id := r.PathValue("id")
	var exists string
	if err = tx.QueryRowContext(r.Context(), "SELECT id FROM inventories WHERE id=? AND deleted_at IS NULL", id).Scan(&exists); err == sql.ErrNoRows {
		return &apiError{404, "库存池不存在或已删除"}
	} else if err != nil {
		return err
	}
	added, duplicates := 0, 0
	for _, doc := range input.Items {
		if err = validateContent(doc, false); err != nil {
			return err
		}
		plain, err := encodeContent(doc)
		if err != nil {
			return err
		}
		stockID := newID()
		cipher, err := a.vault.encrypt(plain, "stock:"+stockID)
		if err != nil {
			return err
		}
		// 编辑用块 ID 不改变库存内容的身份，按顺序规范后做同池去重。
		canonical := ContentDocument{Blocks: append([]ContentBlock(nil), doc.Blocks...)}
		for i := range canonical.Blocks {
			canonical.Blocks[i].ID = strconv.Itoa(i + 1)
		}
		fingerprintContent, err := encodeContent(canonical)
		if err != nil {
			return err
		}
		result, err := tx.ExecContext(r.Context(), "INSERT INTO stock_items(id,inventory_id,content,fingerprint,created_at) VALUES(?,?,?,?,?) ON CONFLICT(inventory_id,fingerprint) WHERE deleted_at IS NULL DO NOTHING", stockID, id, cipher, a.vault.fingerprint(id, fingerprintContent), unixNow())
		if err != nil {
			return err
		}
		count, err := result.RowsAffected()
		if err != nil {
			return err
		}
		if count == 1 {
			if err = a.replaceAssetRefs(r.Context(), tx, "stock", stockID, doc); err != nil {
				return err
			}
			added++
		} else {
			duplicates++
		}
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
	rows, err := a.db.QueryContext(r.Context(), "SELECT id,content,status,COALESCE(order_id,''),created_at FROM stock_items WHERE inventory_id=? AND deleted_at IS NULL ORDER BY created_at DESC,id LIMIT 200", inventory)
	if err != nil {
		return err
	}
	defer rows.Close()
	items := []map[string]any{}
	for rows.Next() {
		var id, cipher, status, order string
		var created int64
		if err = rows.Scan(&id, &cipher, &status, &order, &created); err != nil {
			return err
		}
		plain, err := a.vault.decrypt(cipher, "stock:"+id)
		if err != nil {
			return err
		}
		doc, err := decodeContent(plain)
		if err != nil {
			return err
		}
		items = append(items, map[string]any{"id": id, "content": doc, "status": status, "order_id": order, "created_at": created})
	}
	if err = rows.Err(); err != nil {
		return err
	}
	reply(w, 200, items)
	return nil
}

func (a *App) deleteStock(w http.ResponseWriter, r *http.Request) error {
	tx, err := a.db.BeginTx(r.Context(), nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	result, err := tx.ExecContext(r.Context(), `UPDATE stock_items SET deleted_at=? WHERE id=? AND inventory_id=? AND status='available' AND deleted_at IS NULL
 AND inventory_id IN (SELECT id FROM inventories WHERE deleted_at IS NULL)
 AND NOT EXISTS(SELECT 1 FROM delivery_allocations da WHERE da.stock_id=stock_items.id AND da.status IN ('reserved','consumed'))`, unixNow(), r.PathValue("stock"), r.PathValue("id"))
	if err != nil {
		return err
	}
	count, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if count != 1 {
		return bad("只能删除尚未预留或消耗的库存")
	}
	if err = a.replaceAssetRefs(r.Context(), tx, "stock", r.PathValue("stock"), ContentDocument{}); err != nil {
		return err
	}
	if err = tx.Commit(); err != nil {
		return err
	}
	audit("stock_deleted", r.PathValue("stock"))
	reply(w, 200, map[string]bool{"ok": true})
	return nil
}
