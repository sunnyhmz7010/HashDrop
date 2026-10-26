package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
)

type inventoryAllocation struct {
	content   ContentDocument
	resources []stockAllocation
}
type stockAllocation struct {
	stock, inventory, slot string
	consumes               bool
}

type inventoryQuerier interface {
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
}
type inventoryPool struct {
	id                        string
	reusable                  bool
	available, reserved, sold int
}
type inventoryNeed struct {
	slot     string
	quantity int
	pools    []string
}
type inventoryCatalog struct {
	needs     []inventoryNeed
	pools     map[string]inventoryPool
	poolOrder []string
}

func loadInventoryCatalog(ctx context.Context, q inventoryQuerier, p Product) (inventoryCatalog, error) {
	c := inventoryCatalog{pools: map[string]inventoryPool{}}
	if p.DeliveryPlan == nil {
		return c, bad("商品尚未设置交付内容")
	}
	slots := map[string]int{}
	for _, b := range p.DeliveryPlan.Blocks {
		if b.Type == "stock" {
			slots[b.ID] = len(c.needs)
			c.needs = append(c.needs, inventoryNeed{slot: b.ID, quantity: stockQuantity(b)})
		}
	}
	order := "ip.id ASC"
	if p.InventoryStrategy == "newest" {
		order = "ip.id DESC"
	} else if p.InventoryStrategy == "random" {
		order = "RANDOM()"
	}
	rows, err := q.QueryContext(ctx, `SELECT ip.slot_id,i.id,i.reusable,
 (SELECT COUNT(*) FROM stock_items s WHERE s.inventory_id=i.id AND s.status='available' AND s.deleted_at IS NULL),
 (SELECT COUNT(*) FROM stock_items s WHERE s.inventory_id=i.id AND s.status='reserved' AND s.deleted_at IS NULL),
 (SELECT COUNT(*) FROM stock_items s WHERE s.inventory_id=i.id AND s.status='sold' AND s.deleted_at IS NULL)
 FROM inventory_products ip JOIN inventories i ON i.id=ip.inventory_id AND i.deleted_at IS NULL WHERE ip.product_id=? ORDER BY `+order, p.ID)
	if err != nil {
		return c, err
	}
	defer rows.Close()
	for rows.Next() {
		var slot string
		var pool inventoryPool
		if err = rows.Scan(&slot, &pool.id, &pool.reusable, &pool.available, &pool.reserved, &pool.sold); err != nil {
			return c, err
		}
		index, ok := slots[slot]
		if !ok {
			continue
		}
		c.needs[index].pools = append(c.needs[index].pools, pool.id)
		if _, exists := c.pools[pool.id]; !exists {
			c.poolOrder = append(c.poolOrder, pool.id)
		}
		c.pools[pool.id] = pool
	}
	return c, rows.Err()
}

type inventoryFlowEdge struct{ to, reverse, capacity int }

// 在取货项和候选池之间匹配容量，允许后续必需项让已有分配改用备选池。
// 容量按池计算，不展开每条库存；候选边顺序保留商品的选池优先级。
func matchInventory(c inventoryCatalog, quantity int) ([]map[string]int, bool) {
	count := len(c.needs)
	source, sink := count+len(c.poolOrder), count+len(c.poolOrder)+1
	graph := make([][]inventoryFlowEdge, sink+1)
	add := func(from, to, capacity int) int {
		index := len(graph[from])
		reverse := len(graph[to])
		graph[from] = append(graph[from], inventoryFlowEdge{to, reverse, capacity})
		graph[to] = append(graph[to], inventoryFlowEdge{from, index, 0})
		return index
	}
	poolNodes := map[string]int{}
	total := 0
	for _, need := range c.needs {
		total += quantity * need.quantity
	}
	for i, id := range c.poolOrder {
		poolNodes[id] = count + i
		pool := c.pools[id]
		capacity := pool.available
		if pool.reusable && capacity > 0 {
			capacity = total
		}
		add(count+i, sink, capacity)
	}
	edges := make([]map[string]int, count)
	for i, need := range c.needs {
		demand := quantity * need.quantity
		add(source, i, demand)
		edges[i] = map[string]int{}
		for _, id := range need.pools {
			edges[i][id] = add(i, poolNodes[id], demand)
		}
	}
	var augment func(int, int, []bool) int
	augment = func(node, capacity int, seen []bool) int {
		if node == sink {
			return capacity
		}
		seen[node] = true
		for i := range graph[node] {
			edge := graph[node][i]
			if edge.capacity == 0 || seen[edge.to] {
				continue
			}
			sent := augment(edge.to, min(capacity, edge.capacity), seen)
			if sent > 0 {
				graph[node][i].capacity -= sent
				graph[edge.to][edge.reverse].capacity += sent
				return sent
			}
		}
		return 0
	}
	for remaining := total; remaining > 0; {
		sent := augment(source, remaining, make([]bool, len(graph)))
		if sent == 0 {
			return nil, false
		}
		remaining -= sent
	}
	assigned := make([]map[string]int, count)
	for i, need := range c.needs {
		assigned[i] = map[string]int{}
		for _, id := range need.pools {
			assigned[i][id] = quantity*need.quantity - graph[i][edges[i][id]].capacity
		}
	}
	return assigned, true
}

// 与订单插入处于同一事务：全部取货项满足后才会提交预留和复用游标。
func (a *App) allocateInventory(ctx context.Context, tx *sql.Tx, p Product, quantity int, orderID string) ([]inventoryAllocation, error) {
	if quantity < 1 || quantity > 100 {
		return nil, bad("数量须为 1–100")
	}
	c, err := loadInventoryCatalog(ctx, tx, p)
	if err != nil {
		return nil, err
	}
	assigned, enough := matchInventory(c, quantity)
	if !enough {
		return nil, &apiError{409, "库存不足，请调整数量"}
	}
	slots := map[string]int{}
	for i, need := range c.needs {
		slots[need.slot] = i
	}
	items := make([]inventoryAllocation, quantity)
	for i := range items {
		item := &items[i]
		item.content.Blocks = []ContentBlock{}
		encodedBytes := len(`{"blocks":[]}`)
		appendBlock := func(block ContentBlock) error {
			raw, err := json.Marshal(block)
			if err != nil {
				return err
			}
			separator := 0
			if len(item.content.Blocks) > 0 {
				separator = 1
			}
			if len(item.content.Blocks) >= maxContentBlocks || encodedBytes+separator+len(raw) > maxContentBytes {
				return bad("组合后的交付内容超过 1000 个内容块或 1 MB，请调整商品计划")
			}
			encodedBytes += separator + len(raw)
			item.content.Blocks = append(item.content.Blocks, block)
			return nil
		}
		for _, block := range p.DeliveryPlan.Blocks {
			if block.Type != "stock" {
				if err = appendBlock(block); err != nil {
					return nil, err
				}
				continue
			}
			index := slots[block.ID]
			for j := 0; j < stockQuantity(block); j++ {
				selected := ""
				for _, poolID := range c.needs[index].pools {
					if assigned[index][poolID] > 0 {
						selected = poolID
						assigned[index][poolID]--
						break
					}
				}
				if selected == "" {
					return nil, fmt.Errorf("库存容量匹配缺少取货项 %s", block.ID)
				}
				pool := c.pools[selected]
				stockOrder := "created_at,id"
				if pool.reusable {
					stockOrder = "last_allocated,created_at,id"
				}
				var stock, cipher string
				err = tx.QueryRowContext(ctx, "SELECT id,content FROM stock_items WHERE inventory_id=? AND status='available' AND deleted_at IS NULL ORDER BY "+stockOrder+" LIMIT 1", selected).Scan(&stock, &cipher)
				if err == sql.ErrNoRows {
					return nil, &apiError{409, "库存发生变化，请重试"}
				}
				if err != nil {
					return nil, err
				}
				plain, err := a.vault.decrypt(cipher, "stock:"+stock)
				if err != nil {
					return nil, err
				}
				doc, err := decodeContent(plain)
				if err != nil {
					return nil, err
				}
				if err = validateContent(doc, false); err != nil {
					return nil, err
				}
				var result sql.Result
				if pool.reusable {
					result, err = tx.ExecContext(ctx, "UPDATE stock_items SET last_allocated=(SELECT COALESCE(MAX(last_allocated),0)+1 FROM stock_items WHERE inventory_id=?) WHERE id=? AND status='available' AND deleted_at IS NULL", selected, stock)
				} else {
					result, err = tx.ExecContext(ctx, "UPDATE stock_items SET status='reserved',order_id=? WHERE id=? AND inventory_id=? AND status='available' AND deleted_at IS NULL", orderID, stock, selected)
				}
				if err != nil {
					return nil, err
				}
				changed, err := result.RowsAffected()
				if err != nil {
					return nil, err
				}
				if changed != 1 {
					return nil, &apiError{409, "库存发生变化，请重试"}
				}
				item.resources = append(item.resources, stockAllocation{stock: stock, inventory: selected, slot: block.ID, consumes: !pool.reusable})
				for _, child := range doc.Blocks {
					// 每次拼装分配新的块标识，重复条目与固定块不会碰撞。
					child.ID = newID()
					if err = appendBlock(child); err != nil {
						return nil, err
					}
				}
			}
		}
		if err = validateContent(item.content, false); err != nil {
			return nil, err
		}
	}
	return items, nil
}
