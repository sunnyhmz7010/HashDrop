package main

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
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

func bindInventoryProducts(t *testing.T, a *App, inventory string, products ...string) Inventory {
	t.Helper()
	pool, err := scanInventory(a.db.QueryRow(inventorySelect+" WHERE i.id=?", inventory))
	if err != nil {
		t.Fatal(err)
	}
	if products == nil {
		products = []string{}
	}
	body, err := json.Marshal(map[string]any{"name": pool.Name, "kind": pool.Kind, "product_ids": products})
	if err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest("PUT", "/api/admin/inventories/"+inventory, strings.NewReader(string(body)))
	request.SetPathValue("id", inventory)
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	wrap(a.saveInventory)(response, request)
	if response.Code != 200 || json.Unmarshal(response.Body.Bytes(), &pool) != nil {
		t.Fatalf("库存池绑定商品失败: %s", response.Body)
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
	if response.Code != 200 || json.Unmarshal(response.Body.Bytes(), &draft) != nil || draft.InventoryCount != 0 || draft.Kind != "" {
		t.Fatal("无法在库存之外独立保存商品草稿")
	}
	if response = admin("PUT", "/api/admin/products/"+draft.ID, `{"name":"待绑定商品","price":100,"currency":"CNY","active":true}`); response.Code != 200 {
		t.Fatal("无法先保存商品再从库存池绑定")
	}
	if _, _, err := a.reservePricedOrder(context.Background(), orderInput{ProductID: draft.ID, Quantity: 1}); err == nil {
		t.Fatal("未绑定库存的商品可以下单")
	}
	bindInventoryProducts(t, a, pool.ID, draft.ID)
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
	if response = admin("PUT", "/api/admin/products/"+draft.ID, `{"name":"旧窗口","price":100,"currency":"CNY","active":true}`); response.Code != 404 {
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
	bindInventoryProducts(t, a, second)
	bindInventoryProducts(t, a, first, first, second)
	if response := admin("PUT", "/api/admin/products/"+second, `{"name":"第二个商品","price":2345,"currency":"CNY","active":true}`); response.Code != 200 {
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
		if err != nil || product.Available != 0 || product.Reserved != 5 || product.InventoryCount != 1 || product.Kind != "card" {
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
	bindInventoryProducts(t, a, product)
	bindInventoryProducts(t, a, pool.ID, product)
	if response := admin("PUT", "/api/admin/products/"+product, `{"name":"改为固定交付","price":100,"currency":"CNY","active":true}`); response.Code != 200 {
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
	if response := admin("PUT", "/api/admin/inventories/"+product, `{"name":"绑定失效库存","kind":"card","product_ids":["`+product+`"]}`); response.Code != 404 {
		t.Fatal("已删除库存池仍可以绑定商品")
	}
}

func TestMultipleInventoryPoolsAllocateAndDeliverWithoutOverselling(t *testing.T) {
	for _, strategy := range []string{"oldest", "newest", "random"} {
		t.Run(strategy, func(t *testing.T) {
			a, gateway := newTestApp(t)
			first := insertProduct(t, a, "card", 2)
			second := insertProduct(t, a, "account", 1)
			bindInventoryProducts(t, a, second, first, second)
			admin := adminSessionRequest(t, a)
			if response := admin("PUT", "/api/admin/products/"+first, `{"name":"多池商品","price":1234,"currency":"CNY","active":true,"inventory_strategy":"`+strategy+`"}`); response.Code != 200 {
				t.Fatal(response.Body)
			}
			product, err := scanProduct(a.db.QueryRow(productSelect+" WHERE p.id=?", first))
			if err != nil || product.InventoryCount != 2 || product.Available != 3 || product.Kind != "mixed" || product.StockUnlimited {
				t.Fatalf("多池可售库存统计错误: %+v %v", product, err)
			}
			o, _ := reserve(t, a, first, 3)
			var firstPool string
			if err = a.db.QueryRow("SELECT inventory_id FROM deliveries WHERE order_id=? AND ordinal=1", o.ID).Scan(&firstPool); err != nil {
				t.Fatal(err)
			}
			if strategy == "oldest" && firstPool != first || strategy == "newest" && firstPool != second {
				t.Fatal("没有按商品的绑定优先级分配库存", firstPool)
			}
			if o.Kind != "mixed" || o.InventoryID != "" {
				t.Fatal("跨类型、跨库存池订单未记录逐份来源")
			}
			if _, _, err = a.reservePricedOrder(context.Background(), orderInput{ProductID: second, Quantity: 1}); err == nil {
				t.Fatal("多池预留后共享商品仍可超卖")
			}
			bindInventoryProducts(t, a, first)
			bindInventoryProducts(t, a, second)
			syncPaid(t, a, gateway, o)
			results, err := a.deliveryResults(context.Background(), o.ID)
			if err != nil || len(results) != 3 {
				t.Fatal("解绑影响已预留的跨池交付", err)
			}
			var sold, reserved int
			if err = a.db.QueryRow("SELECT SUM(status='sold'),SUM(status='reserved') FROM stock_items WHERE order_id=?", o.ID).Scan(&sold, &reserved); err != nil || sold != 3 || reserved != 0 {
				t.Fatal("多池交付未准确消费预留", err)
			}
		})
	}
}

func TestBindingPrioritySurvivesOrdinarySavesAndChangesOnRebinding(t *testing.T) {
	a, _ := newTestApp(t)
	product := insertProduct(t, a, "card", 0)
	first := createInventoryViaAPI(t, a, "static", "先绑定内容")
	second := createInventoryViaAPI(t, a, "static", "后绑定内容")
	bindInventoryProducts(t, a, first.ID, product)
	bindInventoryProducts(t, a, second.ID, product)
	assertPool := func(expected string) {
		t.Helper()
		o, _ := reserve(t, a, product, 1)
		if o.InventoryID != expected {
			t.Fatal("绑定顺序或空池跳过不正确", o.InventoryID, expected)
		}
	}
	assertPool(first.ID)
	bindInventoryProducts(t, a, first.ID, product)
	assertPool(first.ID)
	bindInventoryProducts(t, a, first.ID)
	bindInventoryProducts(t, a, first.ID, product)
	assertPool(second.ID)
	admin := adminSessionRequest(t, a)
	if response := admin("PUT", "/api/admin/products/"+product, `{"name":"后绑定优先","price":1234,"currency":"CNY","active":true,"inventory_strategy":"newest"}`); response.Code != 200 {
		t.Fatal(response.Body)
	}
	assertPool(first.ID)
	if response := admin("PUT", "/api/admin/products/"+product, `{"name":"非法策略","price":1234,"currency":"CNY","active":true,"inventory_strategy":"invalid"}`); response.Code != 400 {
		t.Fatal("无效的库存分配策略未被拒绝")
	}
}

func TestInvalidInventoryBindingsRollBackPoolChanges(t *testing.T) {
	a, _ := newTestApp(t)
	product := insertProduct(t, a, "card", 1)
	admin := adminSessionRequest(t, a)
	response := admin("PUT", "/api/admin/inventories/"+product, `{"name":"不应保存的名称","kind":"card","product_ids":["`+product+`","missing"]}`)
	if response.Code != 400 {
		t.Fatal("不存在的商品仍可绑定库存池", response.Body)
	}
	pool, err := scanInventory(a.db.QueryRow(inventorySelect+" WHERE i.id=?", product))
	if err != nil || pool.Name == "不应保存的名称" || pool.ProductCount != 1 || pool.ProductIDs[0] != product {
		t.Fatal("绑定失败改变了原库存池或关联", err)
	}
	if response = admin("PUT", "/api/admin/products/"+product, `{"name":"人工商品","price":1234,"currency":"CNY","active":true,"delivery_mode":"manual"}`); response.Code != 200 {
		t.Fatal(response.Body)
	}
	response = admin("PUT", "/api/admin/inventories/"+product, `{"name":"拒绝人工商品","kind":"card","product_ids":["`+product+`"]}`)
	if response.Code != 400 {
		t.Fatal("人工交付商品仍可绑定库存池", response.Body)
	}
}
