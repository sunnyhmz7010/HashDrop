package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"tinyfaka"
)

func createInventoryViaAPI(t *testing.T, a *App, kind, content string) Inventory {
	t.Helper()
	request := adminSessionRequest(t, a)
	body, err := json.Marshal(map[string]any{"name": "独立库存池", "kind": kind, "static_content": content})
	if err != nil {
		t.Fatal(err)
	}
	response := request("POST", "/api/admin/inventories", string(body))
	var pool Inventory
	if response.Code != 200 || json.Unmarshal(response.Body.Bytes(), &pool) != nil {
		t.Fatalf("新建独立库存池失败: %s", response.Body)
	}
	return pool
}

func TestIndependentInventoryLifecycle(t *testing.T) {
	a, _ := newTestApp(t)
	pool := createInventoryViaAPI(t, a, "card", "")
	admin := adminSessionRequest(t, a)
	response := admin("POST", "/api/admin/inventories/"+pool.ID+"/stock", `{"content":"独立-001\n独立-001\n独立-002"}`)
	if response.Code != 200 || !strings.Contains(response.Body.String(), `"added":2`) || !strings.Contains(response.Body.String(), `"duplicates":1`) {
		t.Fatalf("未关联商品的库存无法导入或去重: %s", response.Body)
	}
	var pools []Inventory
	response = admin("GET", "/api/admin/inventories")
	if response.Code != 200 || json.Unmarshal(response.Body.Bytes(), &pools) != nil || len(pools) != 1 || pools[0].Available != 2 || pools[0].ProductCount != 0 {
		t.Fatal("独立库存统计不正确")
	}
	stock := admin("GET", "/api/admin/inventories/"+pool.ID+"/stock")
	var items []struct {
		ID string `json:"id"`
	}
	if stock.Code != 200 || json.Unmarshal(stock.Body.Bytes(), &items) != nil || len(items) != 2 || strings.Contains(stock.Body.String(), "独立-001") {
		t.Fatal("库存明细缺失或回显了敏感内容")
	}
	if response = admin("DELETE", "/api/admin/inventories/"+pool.ID+"/stock/"+items[0].ID); response.Code != 200 {
		t.Fatal(response.Body)
	}
	response = admin("POST", "/api/admin/products", `{"name":"未关联草稿","price":100,"currency":"CNY","active":false}`)
	var draft Product
	if response.Code != 200 || json.Unmarshal(response.Body.Bytes(), &draft) != nil || draft.InventoryID != "" || draft.Kind != "" {
		t.Fatal("无法在库存之外独立保存商品草稿")
	}
	if response = admin("PUT", "/api/admin/products/"+draft.ID, `{"name":"未关联草稿","price":100,"currency":"CNY","active":true}`); response.Code != 400 {
		t.Fatal("没有库存池的商品被上架")
	}
	if response = admin("PUT", "/api/admin/products/"+draft.ID, `{"name":"关联商品","price":100,"currency":"CNY","active":true,"inventory_id":"`+pool.ID+`"}`); response.Code != 200 {
		t.Fatal(response.Body)
	}
	if response = admin("DELETE", "/api/admin/inventories/"+pool.ID); response.Code != 409 {
		t.Fatal("已关联商品的库存池被删除")
	}
	if response = admin("PUT", "/api/admin/inventories/"+pool.ID, `{"name":"改类型","kind":"static","static_content":"不应覆盖"}`); response.Code != 400 {
		t.Fatal("库存池类型可被静默改写")
	}
	if response = admin("DELETE", "/api/admin/products/"+draft.ID); response.Code != 200 {
		t.Fatal(response.Body)
	}
	var overview map[string]int
	response = admin("GET", "/api/admin/overview")
	if json.Unmarshal(response.Body.Bytes(), &overview) != nil || overview["stock"] != 1 {
		t.Fatal("商品删除后独立库存被遗漏或重复统计")
	}
	if response = admin("DELETE", "/api/admin/inventories/"+pool.ID); response.Code != 200 {
		t.Fatal(response.Body)
	}
	if response = admin("POST", "/api/admin/inventories/"+pool.ID+"/stock", `{"content":"删除后禁止导入"}`); response.Code != 404 {
		t.Fatal("已删除库存池仍可写入")
	}
	if response = admin("PUT", "/api/admin/products/"+draft.ID, `{"name":"旧窗口","price":100,"currency":"CNY","active":true,"inventory_id":"`+pool.ID+`"}`); response.Code != 404 {
		t.Fatal("旧编辑窗口恢复了已删除商品")
	}
	unauthorized := httptest.NewRecorder()
	a.routes().ServeHTTP(unauthorized, httptest.NewRequest("GET", "/api/admin/inventories", nil))
	if unauthorized.Code != 401 {
		t.Fatal("独立库存接口缺少管理员鉴权")
	}
}

func TestInventoryCiphertextCannotMoveAcrossPools(t *testing.T) {
	a, _ := newTestApp(t)
	first := insertProduct(t, a, "static", 0)
	second := insertProduct(t, a, "static", 0)
	if _, err := a.db.Exec(`UPDATE inventories SET content=(SELECT content FROM inventories WHERE id=?),
 content_aad=(SELECT content_aad FROM inventories WHERE id=?) WHERE id=?`, first, first, second); err == nil {
		t.Fatal("库存池接受了其他记录的密文认证上下文")
	}
	if _, err := a.db.Exec("UPDATE inventories SET content=(SELECT content FROM inventories WHERE id=?) WHERE id=?", first, second); err != nil {
		t.Fatal(err)
	}
	if _, _, err := a.reservePricedOrder(context.Background(), orderInput{ProductID: second, Quantity: 1}); err == nil {
		t.Fatal("跨库存池替换的密文通过了下单解密")
	}
}

func TestSharedInventoryPreventsOversellingAcrossProducts(t *testing.T) {
	a, gateway := newTestApp(t)
	first := insertProduct(t, a, "card", 5)
	second := insertProduct(t, a, "account", 0)
	admin := adminSessionRequest(t, a)
	if response := admin("PUT", "/api/admin/products/"+second, `{"name":"第二个商品","price":2345,"currency":"CNY","active":true,"inventory_id":"`+first+`"}`); response.Code != 200 {
		t.Fatal(response.Body)
	}
	start := make(chan struct{})
	winners := make(chan Order, 12)
	failures := make(chan error, 12)
	var group sync.WaitGroup
	for i := 0; i < 12; i++ {
		group.Go(func() {
			<-start
			product := first
			if i%2 != 0 {
				product = second
			}
			o, _, err := a.reservePricedOrder(context.Background(), orderInput{ProductID: product, Quantity: 1})
			if err == nil {
				winners <- o
			} else if failure, ok := err.(*apiError); !ok || failure.Status != 409 {
				failures <- err
			}
		})
	}
	close(start)
	group.Wait()
	close(winners)
	close(failures)
	for err := range failures {
		t.Fatal(err)
	}
	if len(winners) != 5 {
		t.Fatalf("共享 5 份库存却创建了 %d 笔订单", len(winners))
	}
	for _, id := range []string{first, second} {
		product, err := scanProduct(a.db.QueryRow(productSelect+" WHERE p.id=?", id))
		if err != nil || product.Available != 0 || product.Reserved != 5 || product.InventoryID != first || product.Kind != "card" {
			t.Fatalf("共享库存没有同步到商品: %+v %v", product, err)
		}
	}
	seen := map[string]bool{}
	for o := range winners {
		syncPaid(t, a, gateway, o)
		results, err := a.deliveryResults(context.Background(), o.ID)
		if err != nil || len(results) != 1 || seen[results[0].Content] {
			t.Fatal("不同商品重复交付了同一份库存")
		}
		seen[results[0].Content] = true
	}
	pool, err := scanInventory(a.db.QueryRow(inventorySelect+" WHERE i.id=?", first))
	if err != nil || pool.ProductCount != 2 || pool.Sold != 5 || pool.Reserved != 0 {
		t.Fatalf("共享库存扣减或关联计数不正确: %+v %v", pool, err)
	}
}

func TestProductRebindingPreservesOriginalDeliveryAndReleasesOriginalPool(t *testing.T) {
	a, gateway := newTestApp(t)
	product := insertProduct(t, a, "card", 2)
	pending, _ := reserve(t, a, product, 1)
	invalid, _ := reserve(t, a, product, 1)
	pool := createInventoryViaAPI(t, a, "static", "新库存池内容")
	admin := adminSessionRequest(t, a)
	if response := admin("PUT", "/api/admin/products/"+product, `{"name":"改为固定交付","price":100,"currency":"CNY","active":true,"inventory_id":"`+pool.ID+`"}`); response.Code != 200 {
		t.Fatal(response.Body)
	}
	if response := admin("DELETE", "/api/admin/inventories/"+product); response.Code != 409 {
		t.Fatal("商品改绑后原订单预留失去保护")
	}
	fresh, _ := reserve(t, a, product, 1)
	if fresh.Kind != "static" || fresh.InventoryID != pool.ID {
		t.Fatal("商品改绑没有应用于新订单")
	}
	if response := admin("PUT", "/api/admin/inventories/"+pool.ID, `{"name":"更新固定内容","kind":"static","static_content":"更新后内容"}`); response.Code != 200 {
		t.Fatal(response.Body)
	}
	syncPaid(t, a, gateway, pending)
	syncPaid(t, a, gateway, fresh)
	oldResults, err := a.deliveryResults(context.Background(), pending.ID)
	if err != nil || len(oldResults) != 1 || !strings.HasPrefix(oldResults[0].Content, "卡密-") {
		t.Fatal("改绑后旧订单未使用原库存")
	}
	newResults, err := a.deliveryResults(context.Background(), fresh.ID)
	if err != nil || len(newResults) != 1 || newResults[0].Content != "新库存池内容" {
		t.Fatal("更新库存内容覆盖了既有订单快照")
	}
	if err = a.terminateOrder(context.Background(), invalid.ID, "invalid", "测试可靠终止"); err != nil {
		t.Fatal(err)
	}
	original, err := scanInventory(a.db.QueryRow(inventorySelect+" WHERE i.id=?", product))
	if err != nil || original.Available != 1 || original.Reserved != 0 || original.Sold != 1 {
		t.Fatalf("改绑后订单没有向原库存池归还预留: %+v %v", original, err)
	}
	if response := admin("DELETE", "/api/admin/inventories/"+product); response.Code != 200 {
		t.Fatal(response.Body)
	}
	if response := admin("PUT", "/api/admin/products/"+product, `{"name":"绑定失效库存","price":100,"currency":"CNY","active":true,"inventory_id":"`+product+`"}`); response.Code != 400 {
		t.Fatal("商品可以关联已删除库存池")
	}
}

func TestInventoryMigrationPreservesCiphertextsAndPendingOrders(t *testing.T) {
	a, gateway := newTestApp(t)
	path := filepath.Join(t.TempDir(), "before-inventories.db")
	old, err := sql.Open("sqlite", databaseURI(path))
	if err != nil {
		t.Fatal(err)
	}
	defer old.Close()
	for _, file := range []string{"001_init", "002_gateway_orders", "003_gateway_identity", "004_remove_service_products", "005_product_details", "006_hidden_products", "007_deleted_products", "008_promotions"} {
		schema, err := tinyfaka.Assets.ReadFile("migrations/" + file + ".sql")
		if err != nil {
			t.Fatal(err)
		}
		if _, err = old.Exec(string(schema)); err != nil {
			t.Fatal(err)
		}
	}
	staticCipher, err := a.vault.encrypt("迁移前固定内容", "product:legacy-static")
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range []struct{ id, kind, content string }{{"legacy-static", "static", staticCipher}, {"legacy-stock", "card", ""}} {
		if _, err = old.Exec("INSERT INTO products(id,name,price,currency,kind,active,content,created_at,updated_at) VALUES(?,?,100,'CNY',?,1,?,1,1)", p.id, p.id, p.kind, p.content); err != nil {
			t.Fatal(err)
		}
	}
	for _, o := range []struct{ id, kind, product, payment, delivery, reservation string }{
		{"old-pending", "card", "legacy-stock", "creating", "pending", "reserved"},
		{"old-paid", "card", "legacy-stock", "paid", "done", "consumed"},
	} {
		if _, err = old.Exec(`INSERT INTO orders(id,product_id,product_name,kind,unit_price,currency,quantity,total,token_hash,payment_status,delivery_status,reservation_status,created_at,updated_at) VALUES(?,?,?,?,100,'CNY',1,100,?,?,?,?,1,1)`, o.id, o.product, o.product, o.kind, tokenHash(o.id), o.payment, o.delivery, o.reservation); err != nil {
			t.Fatal(err)
		}
	}
	for _, stock := range []struct{ id, state, order string }{{"available", "available", ""}, {"reserved", "reserved", "old-pending"}, {"sold", "sold", "old-paid"}} {
		cipher, err := a.vault.encrypt("迁移前库存-"+stock.id, "stock:"+stock.id)
		if err != nil {
			t.Fatal(err)
		}
		var order any
		if stock.order != "" {
			order = stock.order
		}
		if _, err = old.Exec("INSERT INTO stock_items(id,product_id,content,fingerprint,status,order_id,created_at) VALUES(?,'legacy-stock',?,?,?,?,1)", stock.id, cipher, a.vault.fingerprint("legacy-stock", "迁移前库存-"+stock.id), stock.state, order); err != nil {
			t.Fatal(err)
		}
		if stock.order != "" {
			result, state := "", "pending"
			if stock.state == "sold" {
				state = "done"
				result, err = a.vault.encrypt("原领取结果", "delivery:delivery-sold")
				if err != nil {
					t.Fatal(err)
				}
			}
			if _, err = old.Exec("INSERT INTO deliveries(id,order_id,ordinal,stock_id,result,status,updated_at) VALUES(?,?,1,?,?,?,1)", "delivery-"+stock.id, stock.order, stock.id, result, state); err != nil {
				t.Fatal(err)
			}
		}
	}
	if err = old.Close(); err != nil {
		t.Fatal(err)
	}
	if err = a.db.Close(); err != nil {
		t.Fatal(err)
	}
	a.db, err = openDatabase(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { a.db.Close() })
	var version, foreignKeys int
	if err = a.db.QueryRow("PRAGMA user_version").Scan(&version); err != nil || version != 10 {
		t.Fatal("库存迁移版本无效", err)
	}
	if err = a.db.QueryRow("PRAGMA foreign_keys").Scan(&foreignKeys); err != nil || foreignKeys != 1 {
		t.Fatal("迁移后未开启外键", err)
	}
	for _, column := range []string{"kind", "content"} {
		rows, err := a.db.Query("SELECT " + column + " FROM products")
		if err == nil {
			rows.Close()
			t.Fatal("商品仍持有交付类型或内容")
		}
	}
	pool, err := scanInventory(a.db.QueryRow(inventorySelect + " WHERE i.id='legacy-stock'"))
	if err != nil || pool.Available != 1 || pool.Reserved != 1 || pool.Sold != 1 {
		t.Fatalf("迁移改变库存状态: %+v %v", pool, err)
	}
	admin := adminSessionRequest(t, a)
	duplicate := admin("POST", "/api/admin/inventories/legacy-stock/stock", `{"content":"迁移前库存-available"}`)
	if duplicate.Code != 200 || !strings.Contains(duplicate.Body.String(), `"duplicates":1`) {
		t.Fatal("迁移破坏了库存去重", duplicate.Body)
	}
	pending, err := a.loadOrder(context.Background(), "old-pending")
	if err != nil || pending.InventoryID != "legacy-stock" {
		t.Fatal("未记录旧订单库存来源", err)
	}
	syncPaid(t, a, gateway, pending)
	results, err := a.deliveryResults(context.Background(), pending.ID)
	if err != nil || len(results) != 1 || results[0].Content != "迁移前库存-reserved" {
		t.Fatal("迁移破坏了旧预留交付", err)
	}
	results, err = a.deliveryResults(context.Background(), "old-paid")
	if err != nil || len(results) != 1 || results[0].Content != "原领取结果" {
		t.Fatal("迁移破坏了原领取结果", err)
	}
	static, _ := reserve(t, a, "legacy-static", 1)
	syncPaid(t, a, gateway, static)
	results, err = a.deliveryResults(context.Background(), static.ID)
	if err != nil || len(results) != 1 || results[0].Content != "迁移前固定内容" {
		t.Fatal("迁移后不能解密旧商品固定内容", err)
	}
	if response := admin("PUT", "/api/admin/inventories/legacy-static", `{"name":"修改库存","kind":"static","static_content":"新固定内容"}`); response.Code != 200 {
		t.Fatal(response.Body)
	}
	static, _ = reserve(t, a, "legacy-static", 1)
	syncPaid(t, a, gateway, static)
	results, err = a.deliveryResults(context.Background(), static.ID)
	if err != nil || len(results) != 1 || results[0].Content != "新固定内容" {
		t.Fatal("迁移后更新内容的认证上下文不正确", err)
	}
}
