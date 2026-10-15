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
