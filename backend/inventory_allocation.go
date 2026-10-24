package main

import (
	"context"
	"database/sql"
)

type inventoryAllocation struct {
	inventory, kind, stock, content string
}

// 在下单的同一个写事务中按池排序取货，库存不足时继续下一池。
func (a *App) allocateInventory(ctx context.Context, tx *sql.Tx, p Product, quantity int) ([]inventoryAllocation, error) {
	if p.DeliveryMode == "manual" {
		items := make([]inventoryAllocation, quantity)
		for i := range items {
			items[i].kind = "manual"
		}
		return items, nil
	}
	order := "ip.id ASC"
	switch p.InventoryStrategy {
	case "newest":
		order = "ip.id DESC"
	case "random":
		order = "RANDOM()"
	}
	rows, err := tx.QueryContext(ctx, `SELECT i.id,i.kind,i.content FROM inventory_products ip
 JOIN inventories i ON i.id=ip.inventory_id WHERE ip.product_id=? AND i.deleted_at IS NULL ORDER BY `+order, p.ID)
	if err != nil {
		return nil, err
	}
	type pool struct{ id, kind, content string }
	pools := []pool{}
	for rows.Next() {
		var i pool
		if err = rows.Scan(&i.id, &i.kind, &i.content); err != nil {
			rows.Close()
			return nil, err
		}
		pools = append(pools, i)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, err
	}
	if len(pools) == 0 {
		return nil, bad("商品尚未关联可用库存池")
	}
	items := make([]inventoryAllocation, 0, quantity)
	for _, pool := range pools {
		if len(items) == quantity {
			break
		}
		if pool.kind == "static" {
			if pool.content == "" {
				continue
			}
			plain, err := a.vault.decrypt(pool.content, "inventory:"+pool.id)
			if err != nil {
				return nil, err
			}
			for len(items) < quantity {
				items = append(items, inventoryAllocation{inventory: pool.id, kind: pool.kind, content: plain})
			}
			continue
		}
		rows, err := tx.QueryContext(ctx, "SELECT id FROM stock_items WHERE inventory_id=? AND status='available' ORDER BY created_at,id LIMIT ?", pool.id, quantity-len(items))
		if err != nil {
			return nil, err
		}
		for rows.Next() {
			var stock string
			if err = rows.Scan(&stock); err != nil {
				rows.Close()
				return nil, err
			}
			items = append(items, inventoryAllocation{inventory: pool.id, kind: pool.kind, stock: stock})
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return nil, err
		}
	}
	if len(items) != quantity {
		return nil, &apiError{409, "库存不足，请调整数量"}
	}
	return items, nil
}
