package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync"
	"testing"
)

var catalogAdminSessions sync.Map

// 同一测试复用已登录会话，避免把库存行为测试变成登录限流测试。
func catalogAdmin(t *testing.T, a *App) func(string, string, ...string) *httptest.ResponseRecorder {
	t.Helper()
	if request, ok := catalogAdminSessions.Load(a); ok {
		return request.(func(string, string, ...string) *httptest.ResponseRecorder)
	}
	request := adminSessionRequest(t, a)
	catalogAdminSessions.Store(a, request)
	t.Cleanup(func() { catalogAdminSessions.Delete(a) })
	return request
}

func createInventoryViaAPI(t *testing.T, a *App, reusable bool) Inventory {
	t.Helper()
	body, _ := json.Marshal(map[string]any{"name": "独立库存池", "reusable": reusable, "bindings": []InventoryBinding{}})
	response := catalogAdmin(t, a)("POST", "/api/admin/inventories", string(body))
	var pool Inventory
	if response.Code != 200 || json.Unmarshal(response.Body.Bytes(), &pool) != nil {
		t.Fatalf("新建库存池失败: %s", response.Body)
	}
	return pool
}
func bindInventorySlots(t *testing.T, a *App, inventory string, bindings ...InventoryBinding) Inventory {
	t.Helper()
	pool, err := scanInventory(a.db.QueryRow(inventorySelect+" WHERE i.id=?", inventory))
	if err != nil {
		t.Fatal(err)
	}
	if bindings == nil {
		bindings = []InventoryBinding{}
	}
	body, _ := json.Marshal(map[string]any{"name": pool.Name, "bindings": bindings})
	response := catalogAdmin(t, a)("PUT", "/api/admin/inventories/"+inventory, string(body))
	if response.Code != 200 || json.Unmarshal(response.Body.Bytes(), &pool) != nil {
		t.Fatalf("绑定库存池失败: %s", response.Body)
	}
	return pool
}
func bindInventoryProducts(t *testing.T, a *App, inventory string, products ...string) Inventory {
	t.Helper()
	bindings := []InventoryBinding{}
	for _, product := range products {
		bindings = append(bindings, InventoryBinding{ProductID: product, SlotID: "stock"})
	}
	return bindInventorySlots(t, a, inventory, bindings...)
}
func importDocuments(t *testing.T, a *App, inventory string, docs ...ContentDocument) map[string]int {
	t.Helper()
	body, _ := json.Marshal(map[string]any{"items": docs})
	response := catalogAdmin(t, a)("POST", "/api/admin/inventories/"+inventory+"/stock", string(body))
	var result map[string]int
	if response.Code != 200 || json.Unmarshal(response.Body.Bytes(), &result) != nil {
		t.Fatalf("导入库存失败: %s", response.Body)
	}
	return result
}
func readCatalogProduct(t *testing.T, a *App, id string) Product {
	t.Helper()
	product, err := scanProduct(a.db.QueryRow(productSelect+" WHERE p.id=?", id))
	if err == nil {
		err = a.hydrateProduct(context.Background(), a.db, &product)
	}
	if err != nil {
		t.Fatal(err)
	}
	return product
}
func saveDeliveryPlan(t *testing.T, a *App, id string, plan ContentDocument) {
	t.Helper()
	body, _ := json.Marshal(map[string]any{"name": "组合商品", "price": 1234, "currency": "CNY", "active": true, "delivery_plan": plan})
	if response := catalogAdmin(t, a)("PUT", "/api/admin/products/"+id, string(body)); response.Code != 200 {
		t.Fatal(response.Body)
	}
}
func allocationPool(t *testing.T, a *App, order string, ordinal int) string {
	t.Helper()
	var pool string
	if err := a.db.QueryRow("SELECT da.inventory_id FROM delivery_allocations da JOIN deliveries d ON d.id=da.delivery_id WHERE d.order_id=? AND d.ordinal=? ORDER BY da.ordinal LIMIT 1", order, ordinal).Scan(&pool); err != nil {
		t.Fatal(err)
	}
	return pool
}

func TestInventoryIndependentCRUDAndSemanticDeduplication(t *testing.T) {
	a, _ := newTestApp(t)
	admin := catalogAdmin(t, a)
	pool := createInventoryViaAPI(t, a, false)
	first, same := testDocument("独立-001"), testDocument("独立-001")
	same.Blocks[0].ID = "another-editor-id"
	result := importDocuments(t, a, pool.ID, first, same, testDocument("独立-002"))
	if result["added"] != 2 || result["duplicates"] != 1 {
		t.Fatal("块ID影响语义去重", result)
	}
	var pools []Inventory
	if response := admin("GET", "/api/admin/inventories"); response.Code != 200 || json.Unmarshal(response.Body.Bytes(), &pools) != nil || len(pools) != 1 || pools[0].Available != 2 || pools[0].ProductCount != 0 {
		t.Fatal("独立库存列表不正确")
	}
	stock := admin("GET", "/api/admin/inventories/"+pool.ID+"/stock")
	var items []struct {
		ID      string          `json:"id"`
		Content ContentDocument `json:"content"`
	}
	if stock.Code != 200 || json.Unmarshal(stock.Body.Bytes(), &items) != nil || len(items) != 2 || len(items[0].Content.Blocks) != 1 {
		t.Fatal("管理员无法查看结构化库存")
	}
	unauthorized := httptest.NewRecorder()
	a.routes().ServeHTTP(unauthorized, httptest.NewRequest("GET", "/api/admin/inventories/"+pool.ID+"/stock", nil))
	if unauthorized.Code != 401 || strings.Contains(unauthorized.Body.String(), "独立-001") {
		t.Fatal("未授权泄露库存")
	}
	if response := admin("DELETE", "/api/admin/inventories/"+pool.ID+"/stock/"+items[0].ID); response.Code != 200 {
		t.Fatal(response.Body)
	}
	var deleted bool
	if err := a.db.QueryRow("SELECT deleted_at IS NOT NULL FROM stock_items WHERE id=?", items[0].ID).Scan(&deleted); err != nil || !deleted {
		t.Fatal("删除库存未保留历史行", err)
	}
	if result := importDocuments(t, a, pool.ID, items[0].Content); result["added"] != 1 || result["duplicates"] != 0 {
		t.Fatal("删除未使用库存后不能重新导入", result)
	}
	product := insertProduct(t, a, "card", 0)
	if _, _, err := a.reservePricedOrder(context.Background(), orderInput{ProductID: product, Quantity: 1}); err == nil {
		t.Fatal("缺货仍可下单")
	}
	bindInventoryProducts(t, a, pool.ID, product)
	if response := admin("DELETE", "/api/admin/inventories/"+pool.ID); response.Code != 409 {
		t.Fatal("已绑定库存可删除")
	}
	if response := admin("DELETE", "/api/admin/products/"+product); response.Code != 200 {
		t.Fatal(response.Body)
	}
	if response := admin("DELETE", "/api/admin/inventories/"+pool.ID); response.Code != 200 {
		t.Fatal(response.Body)
	}
	body, _ := json.Marshal(map[string]any{"items": []ContentDocument{testDocument("删除后禁止导入")}})
	if response := admin("POST", "/api/admin/inventories/"+pool.ID+"/stock", string(body)); response.Code != 404 {
		t.Fatal("已删除池仍可导入")
	}
	if response := admin("PUT", "/api/admin/inventories/"+pool.ID, `{"name":"旧窗口"}`); response.Code != 404 {
		t.Fatal("已删除池可恢复")
	}
}

func TestProductAndStockCiphertextCannotMoveAcrossOwners(t *testing.T) {
	a, _ := newTestApp(t)
	first := insertProduct(t, a, "static", 0)
	second := insertProduct(t, a, "static", 0)
	if _, err := a.db.Exec("UPDATE products SET delivery_plan=(SELECT delivery_plan FROM products WHERE id=?) WHERE id=?", first, second); err != nil {
		t.Fatal(err)
	}
	if _, _, err := a.reservePricedOrder(context.Background(), orderInput{ProductID: second, Quantity: 1}); err == nil {
		t.Fatal("跨商品移植计划密文成功")
	}
	stocked := insertProduct(t, a, "card", 2)
	if _, err := a.db.Exec("UPDATE stock_items SET content=(SELECT content FROM stock_items WHERE inventory_id=? ORDER BY id LIMIT 1) WHERE inventory_id=?", stocked, stocked); err != nil {
		t.Fatal(err)
	}
	if _, _, err := a.reservePricedOrder(context.Background(), orderInput{ProductID: stocked, Quantity: 2}); err == nil {
		t.Fatal("跨库存条目移植密文成功")
	}
	if product := readCatalogProduct(t, a, stocked); product.Available != 2 || product.Reserved != 0 {
		t.Fatal("解密失败没有回滚库存", product)
	}
}

func TestSharedInventoryPreventsOversellingAcrossProducts(t *testing.T) {
	a, gateway := newTestApp(t)
	first := insertProduct(t, a, "card", 5)
	second := insertProduct(t, a, "card", 0)
	bindInventoryProducts(t, a, second)
	bindInventoryProducts(t, a, first, first, second)
	start := make(chan struct{})
	winners, failures := make(chan Order, 12), make(chan error, 12)
	var group sync.WaitGroup
	for i := 0; i < 12; i++ {
		group.Go(func() {
			<-start
			product := first
			if i%2 != 0 {
				product = second
			}
			order, _, err := a.reservePricedOrder(context.Background(), orderInput{ProductID: product, Quantity: 1})
			if err == nil {
				winners <- order
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
		t.Fatalf("共享5份库存创建%d笔订单", len(winners))
	}
	for _, id := range []string{first, second} {
		product := readCatalogProduct(t, a, id)
		if product.Available != 0 || product.Reserved != 5 || product.InventoryCount != 1 {
			t.Fatalf("共享库存统计不正确: %+v", product)
		}
	}
	seen := map[string]bool{}
	for order := range winners {
		syncPaid(t, a, gateway, order)
		results, err := a.deliveryResults(context.Background(), order.ID)
		if err != nil || len(results) != 1 {
			t.Fatal(err)
		}
		text := deliveryText(results[0].Content)
		if text == "" || seen[text] {
			t.Fatal("不同商品重复交付同一库存")
		}
		seen[text] = true
	}
	pool, err := scanInventory(a.db.QueryRow(inventorySelect+" WHERE i.id=?", first))
	if err != nil || pool.ProductCount != 2 || pool.Sold != 5 || pool.Reserved != 0 {
		t.Fatal("共享库存消费不正确", pool, err)
	}
}

func TestProductRebindingPreservesOriginalDeliveryAndReleasesOriginalPool(t *testing.T) {
	a, gateway := newTestApp(t)
	product := insertProduct(t, a, "card", 2)
	pending, _ := reserve(t, a, product, 1)
	invalid, _ := reserve(t, a, product, 1)
	pool := createInventoryViaAPI(t, a, true)
	importDocuments(t, a, pool.ID, testDocument("新库存池内容"))
	bindInventoryProducts(t, a, product)
	bindInventoryProducts(t, a, pool.ID, product)
	admin := catalogAdmin(t, a)
	if response := admin("DELETE", "/api/admin/inventories/"+product); response.Code != 409 {
		t.Fatal("旧预留失去保护")
	}
	fresh, _ := reserve(t, a, product, 1)
	if allocationPool(t, a, fresh.ID, 1) != pool.ID {
		t.Fatal("新订单未使用新绑定")
	}
	bindInventoryProducts(t, a, pool.ID)
	if response := admin("DELETE", "/api/admin/inventories/"+pool.ID); response.Code != 409 {
		t.Fatal("复用库存预留未保护库存池")
	}
	syncPaid(t, a, gateway, pending)
	syncPaid(t, a, gateway, fresh)
	oldResults, err := a.deliveryResults(context.Background(), pending.ID)
	if err != nil || len(oldResults) != 1 || !strings.HasPrefix(deliveryText(oldResults[0].Content), "卡密-") {
		t.Fatal("旧订单未使用原库存", err)
	}
	newResults, err := a.deliveryResults(context.Background(), fresh.ID)
	if err != nil || len(newResults) != 1 || deliveryText(newResults[0].Content) != "新库存池内容" {
		t.Fatal("解绑改变已有快照", err)
	}
	if err = a.terminateOrder(context.Background(), invalid.ID, "invalid", "测试可靠终止"); err != nil {
		t.Fatal(err)
	}
	original, err := scanInventory(a.db.QueryRow(inventorySelect+" WHERE i.id=?", product))
	if err != nil || original.Available != 1 || original.Reserved != 0 || original.Sold != 1 {
		t.Fatal("未向原池归还预留", original, err)
	}
	if response := admin("DELETE", "/api/admin/inventories/"+product); response.Code != 200 {
		t.Fatal(response.Body)
	}
}

func TestMultipleInventoryPoolsAllocateAndDeliverWithoutOverselling(t *testing.T) {
	for _, strategy := range []string{"oldest", "newest", "random"} {
		t.Run(strategy, func(t *testing.T) {
			a, gateway := newTestApp(t)
			first, second := insertProduct(t, a, "card", 2), insertProduct(t, a, "card", 1)
			bindInventoryProducts(t, a, second, first, second)
			admin := catalogAdmin(t, a)
			if response := admin("PUT", "/api/admin/products/"+first, `{"name":"多池商品","price":1234,"currency":"CNY","active":true,"inventory_strategy":"`+strategy+`"}`); response.Code != 200 {
				t.Fatal(response.Body)
			}
			product := readCatalogProduct(t, a, first)
			if product.InventoryCount != 2 || product.Available != 3 || product.StockUnlimited {
				t.Fatal("多池可售统计错误", product)
			}
			order, _ := reserve(t, a, first, 3)
			source := allocationPool(t, a, order.ID, 1)
			if strategy == "oldest" && source != first || strategy == "newest" && source != second {
				t.Fatal("未按策略选择库存", source)
			}
			if _, _, err := a.reservePricedOrder(context.Background(), orderInput{ProductID: second, Quantity: 1}); err == nil {
				t.Fatal("共享商品可以超卖")
			}
			bindInventoryProducts(t, a, first)
			bindInventoryProducts(t, a, second)
			syncPaid(t, a, gateway, order)
			results, err := a.deliveryResults(context.Background(), order.ID)
			if err != nil || len(results) != 3 {
				t.Fatal("解绑影响跨池交付", err)
			}
			var sold int
			if err = a.db.QueryRow("SELECT COUNT(*) FROM stock_items WHERE order_id=? AND status='sold'", order.ID).Scan(&sold); err != nil || sold != 3 {
				t.Fatal("未消费完整预留", sold, err)
			}
		})
	}
}

func TestBindingPrioritySurvivesOrdinarySavesAndChangesOnRebinding(t *testing.T) {
	a, _ := newTestApp(t)
	product := insertProduct(t, a, "card", 0)
	first, second := createInventoryViaAPI(t, a, true), createInventoryViaAPI(t, a, true)
	importDocuments(t, a, first.ID, testDocument("先绑定"))
	importDocuments(t, a, second.ID, testDocument("后绑定"))
	bindInventoryProducts(t, a, first.ID, product)
	bindInventoryProducts(t, a, second.ID, product)
	assertPool := func(expected string) {
		t.Helper()
		order, _ := reserve(t, a, product, 1)
		if actual := allocationPool(t, a, order.ID, 1); actual != expected {
			t.Fatal("绑定优先级错误", actual, expected)
		}
	}
	assertPool(first.ID)
	bindInventoryProducts(t, a, first.ID, product)
	assertPool(first.ID)
	bindInventoryProducts(t, a, first.ID)
	bindInventoryProducts(t, a, first.ID, product)
	assertPool(second.ID)
	if response := catalogAdmin(t, a)("PUT", "/api/admin/products/"+product, `{"name":"后绑定优先","price":1234,"currency":"CNY","active":true,"inventory_strategy":"newest"}`); response.Code != 200 {
		t.Fatal(response.Body)
	}
	assertPool(first.ID)
}

func TestInvalidInventoryBindingsRollBackPoolChanges(t *testing.T) {
	a, _ := newTestApp(t)
	product := insertProduct(t, a, "card", 1)
	admin := catalogAdmin(t, a)
	for _, invalid := range []InventoryBinding{{ProductID: "missing", SlotID: "stock"}, {ProductID: product, SlotID: "missing"}} {
		body, _ := json.Marshal(map[string]any{"name": "不应保存", "reusable": true, "bindings": []InventoryBinding{{ProductID: product, SlotID: "stock"}, invalid}})
		if response := admin("PUT", "/api/admin/inventories/"+product, string(body)); response.Code != 400 {
			t.Fatal("无效绑定未拒绝", response.Body)
		}
		pool, err := scanInventory(a.db.QueryRow(inventorySelect+" WHERE i.id=?", product))
		if err != nil || pool.Name == "不应保存" || pool.Reusable || pool.ProductCount != 1 {
			t.Fatal("失败改变库存池", pool, err)
		}
	}
	saveDeliveryPlan(t, a, product, testDocument("已改成固定内容"))
	pool, err := scanInventory(a.db.QueryRow(inventorySelect+" WHERE i.id=?", product))
	if err != nil || len(pool.Bindings) != 0 {
		t.Fatal("删除取货项未移除失效绑定", err)
	}
	if p := readCatalogProduct(t, a, product); !p.StockUnlimited {
		t.Fatal("纯固定计划不应依赖库存")
	}
}

func TestSharedPoolAcrossSlotsCapacityAndAlternativeMatching(t *testing.T) {
	a, gateway := newTestApp(t)
	product := insertProduct(t, a, "card", 3)
	saveDeliveryPlan(t, a, product, ContentDocument{Blocks: []ContentBlock{{ID: "flexible", Type: "stock"}, {ID: "required", Type: "stock"}}})
	bindInventorySlots(t, a, product, InventoryBinding{ProductID: product, SlotID: "flexible"}, InventoryBinding{ProductID: product, SlotID: "required"})
	if p := readCatalogProduct(t, a, product); p.Available != 1 || p.InventoryCount != 1 {
		t.Fatal("共享池跨取货项虚增可售数", p)
	}
	if _, _, err := a.reservePricedOrder(context.Background(), orderInput{ProductID: product, Quantity: 2}); err == nil {
		t.Fatal("3条库存可满足4次消耗")
	}
	backup := createInventoryViaAPI(t, a, false)
	importDocuments(t, a, backup.ID, testDocument("备选-A"), testDocument("备选-B"), testDocument("备选-C"))
	bindInventorySlots(t, a, backup.ID, InventoryBinding{ProductID: product, SlotID: "flexible"})
	if p := readCatalogProduct(t, a, product); p.Available != 3 {
		t.Fatal("备选池匹配错误", p)
	}
	order, _ := reserve(t, a, product, 3)
	var mismatches int
	if err := a.db.QueryRow("SELECT COUNT(*) FROM delivery_allocations da JOIN deliveries d ON d.id=da.delivery_id WHERE d.order_id=? AND ((da.slot_id='required' AND da.inventory_id<>?) OR (da.slot_id='flexible' AND da.inventory_id<>?))", order.ID, product, backup.ID).Scan(&mismatches); err != nil || mismatches != 0 {
		t.Fatal("未给独占取货项保留共享池", mismatches, err)
	}
	syncPaid(t, a, gateway, order)
	results, err := a.deliveryResults(context.Background(), order.ID)
	if err != nil || len(results) != 3 {
		t.Fatal(err)
	}
	for _, result := range results {
		if result.Content == nil || len(result.Content.Blocks) != 2 {
			t.Fatal("一份组合未整体完成")
		}
	}
}

func TestReusableInventoryCyclesAndPreservesReservationSnapshots(t *testing.T) {
	a, gateway := newTestApp(t)
	product := insertProduct(t, a, "card", 0)
	pool := createInventoryViaAPI(t, a, true)
	importDocuments(t, a, pool.ID, testDocument("复用-A"), testDocument("复用-B"))
	bindInventoryProducts(t, a, pool.ID, product)
	if p := readCatalogProduct(t, a, product); !p.StockUnlimited {
		t.Fatal("复用池未无限供给")
	}
	order, key := reserve(t, a, product, 3)
	var stockIDs []string
	rows, err := a.db.Query("SELECT da.stock_id FROM delivery_allocations da JOIN deliveries d ON d.id=da.delivery_id WHERE d.order_id=? ORDER BY d.ordinal", order.ID)
	if err != nil {
		t.Fatal(err)
	}
	for rows.Next() {
		var id string
		if err = rows.Scan(&id); err != nil {
			t.Fatal(err)
		}
		stockIDs = append(stockIDs, id)
	}
	rows.Close()
	if len(stockIDs) != 3 || stockIDs[0] == stockIDs[1] || stockIDs[0] != stockIDs[2] {
		t.Fatal("复用池未轮转", stockIDs)
	}
	var cursorBefore int
	if err = a.db.QueryRow("SELECT SUM(last_allocated) FROM stock_items WHERE inventory_id=?", pool.ID).Scan(&cursorBefore); err != nil {
		t.Fatal(err)
	}
	recovered, _, err := a.reservePricedOrder(context.Background(), orderInput{ProductID: product, Quantity: 3, Key: key, RequestID: order.ID})
	if err != nil || recovered.ID != order.ID {
		t.Fatal("重复下单请求不能恢复原复用分配", err)
	}
	syncPaid(t, a, gateway, order)
	before, err := a.deliveryResults(context.Background(), order.ID)
	if err != nil {
		t.Fatal(err)
	}
	saved, err := a.loadOrder(context.Background(), order.ID)
	if err != nil {
		t.Fatal(err)
	}
	if err = a.deliverOrder(context.Background(), saved); err != nil {
		t.Fatal(err)
	}
	after, err := a.deliveryResults(context.Background(), order.ID)
	if err != nil || !reflect.DeepEqual(before, after) {
		t.Fatal("重试重抽复用内容", err)
	}
	var available, cursorAfter int
	if err = a.db.QueryRow("SELECT SUM(status='available'),SUM(last_allocated) FROM stock_items WHERE inventory_id=?", pool.ID).Scan(&available, &cursorAfter); err != nil || available != 2 || cursorAfter != cursorBefore {
		t.Fatal("重复交付消耗复用库存或推进游标", err)
	}
	if response := catalogAdmin(t, a)("DELETE", "/api/admin/inventories/"+pool.ID+"/stock/"+stockIDs[0]); response.Code != 400 {
		t.Fatal("历史交付引用的复用库存被删除")
	}
	next, _ := reserve(t, a, product, 1)
	var nextStock string
	if err = a.db.QueryRow("SELECT da.stock_id FROM delivery_allocations da JOIN deliveries d ON d.id=da.delivery_id WHERE d.order_id=?", next.ID).Scan(&nextStock); err != nil || nextStock != stockIDs[1] {
		t.Fatal("游标没有跨订单保留", err)
	}
}

func TestMixedDocumentKeepsFixedContentIndependentFromStock(t *testing.T) {
	a, gateway := newTestApp(t)
	product := insertProduct(t, a, "card", 2)
	file := uploadTestAsset(t, a, "教程.pdf", []byte("%PDF-1.7\ntraining-document"))
	plan := ContentDocument{Blocks: []ContentBlock{{ID: "intro", Type: "text", Text: "安装教程"}, {ID: "download", Type: "file", AssetID: file.ID, Label: "软件下载"}, {ID: "url", Type: "link", URL: "https://example.com/help", Label: "在线帮助"}, {ID: "stock", Type: "stock", Quantity: 1}}}
	saveDeliveryPlan(t, a, product, plan)
	order, _ := reserve(t, a, product, 2)
	saveDeliveryPlan(t, a, product, testDocument("后续更改内容"))
	syncPaid(t, a, gateway, order)
	results, err := a.deliveryResults(context.Background(), order.ID)
	if err != nil || len(results) != 2 {
		t.Fatal(err)
	}
	for _, result := range results {
		if result.Content == nil || len(result.Content.Blocks) != 4 {
			t.Fatal("混合内容不完整")
		}
		blocks := result.Content.Blocks
		if blocks[0].Text != "安装教程" || blocks[1].AssetID != file.ID || blocks[2].URL != "https://example.com/help" || !strings.HasPrefix(blocks[3].Text, "卡密-") {
			t.Fatal("固定内容或原始快照变化", blocks)
		}
	}
	var consumed, refs int
	if err = a.db.QueryRow("SELECT COUNT(*) FROM delivery_allocations WHERE status='consumed'").Scan(&consumed); err != nil || consumed != 2 {
		t.Fatal("整个内容包被当作消耗单位", consumed, err)
	}
	if err = a.db.QueryRow("SELECT COUNT(*) FROM asset_refs WHERE owner_type='delivery' AND asset_id=?", file.ID).Scan(&refs); err != nil || refs != 2 {
		t.Fatal("附件未被订单快照保留", refs, err)
	}
}

func TestInsufficientSlotRollsBackOrderAndReusableCursor(t *testing.T) {
	a, _ := newTestApp(t)
	product := insertProduct(t, a, "card", 0)
	saveDeliveryPlan(t, a, product, ContentDocument{Blocks: []ContentBlock{{ID: "repeat", Type: "stock"}, {ID: "broken", Type: "stock"}}})
	repeat, broken := createInventoryViaAPI(t, a, true), createInventoryViaAPI(t, a, false)
	importDocuments(t, a, repeat.ID, testDocument("复用资源"))
	importDocuments(t, a, broken.ID, testDocument("将被损坏的资源"))
	bindInventorySlots(t, a, repeat.ID, InventoryBinding{ProductID: product, SlotID: "repeat"})
	bindInventorySlots(t, a, broken.ID, InventoryBinding{ProductID: product, SlotID: "broken"})
	if _, err := a.db.Exec("UPDATE stock_items SET content='broken-cipher' WHERE inventory_id=?", broken.ID); err != nil {
		t.Fatal(err)
	}
	if _, _, err := a.reservePricedOrder(context.Background(), orderInput{ProductID: product, Quantity: 1}); err == nil {
		t.Fatal("损坏资源仍可下单")
	}
	var orders, allocations, cursor, reserved int
	for query, target := range map[string]*int{"SELECT COUNT(*) FROM orders": &orders, "SELECT COUNT(*) FROM delivery_allocations": &allocations, "SELECT SUM(last_allocated) FROM stock_items": &cursor, "SELECT COUNT(*) FROM stock_items WHERE status='reserved'": &reserved} {
		if err := a.db.QueryRow(query).Scan(target); err != nil {
			t.Fatal(err)
		}
	}
	if orders != 0 || allocations != 0 || cursor != 0 || reserved != 0 {
		t.Fatal("失败留下部分订单、分配或游标", orders, allocations, cursor, reserved)
	}
}

func TestContentValidationRejectsUnsafeOrAmbiguousBlocks(t *testing.T) {
	for name, doc := range map[string]ContentDocument{
		"空文档": {}, "重复ID": {Blocks: []ContentBlock{{ID: "x", Type: "text", Text: "a"}, {ID: "x", Type: "text", Text: "b"}}},
		"脚本链接":   {Blocks: []ContentBlock{{ID: "x", Type: "link", URL: "javascript:alert(1)"}}},
		"无主机链接":  {Blocks: []ContentBlock{{ID: "x", Type: "link", URL: "https:"}}},
		"嵌套库存":   {Blocks: []ContentBlock{{ID: "x", Type: "stock"}}},
		"缺附件":    {Blocks: []ContentBlock{{ID: "x", Type: "file"}}},
		"隐藏额外字段": {Blocks: []ContentBlock{{ID: "x", Type: "text", Text: "a", AssetID: "hidden"}}},
	} {
		t.Run(name, func(t *testing.T) {
			if err := validateContent(doc, false); err == nil {
				t.Fatal("非法内容通过验证")
			}
		})
	}
	for _, n := range []int{0, 1, 100} {
		if err := validateContent(ContentDocument{Blocks: []ContentBlock{{ID: "slot", Type: "stock", Quantity: n}}}, true); err != nil {
			t.Fatal(fmt.Sprintf("有效取货数量%d拒绝", n), err)
		}
	}
	oversized := ContentDocument{}
	for i := 0; i < 11; i++ {
		oversized.Blocks = append(oversized.Blocks, ContentBlock{ID: fmt.Sprintf("slot-%d", i), Type: "stock", Quantity: 100})
	}
	if err := validateContent(oversized, true); err == nil {
		t.Fatal("必然超过结果块数上限的取货计划未提前拒绝")
	}
}

func TestReusablePolicyChangesDoNotRewriteExistingAllocations(t *testing.T) {
	for _, initiallyReusable := range []bool{false, true} {
		t.Run(fmt.Sprintf("initial=%t", initiallyReusable), func(t *testing.T) {
			a, gateway := newTestApp(t)
			product := insertProduct(t, a, "card", 0)
			pool := createInventoryViaAPI(t, a, initiallyReusable)
			importDocuments(t, a, pool.ID, testDocument("原策略内容"))
			bindInventoryProducts(t, a, pool.ID, product)
			original, _ := reserve(t, a, product, 1)
			body, _ := json.Marshal(map[string]any{"name": pool.Name, "reusable": !initiallyReusable})
			response := catalogAdmin(t, a)("PUT", "/api/admin/inventories/"+pool.ID, string(body))
			if response.Code != 200 {
				t.Fatal(response.Body)
			}
			var newer Order
			if initiallyReusable {
				// 新的一次性订单可使用同条资源，旧的可复用分配仍保有自己的结果。
				newer, _ = reserve(t, a, product, 1)
				syncPaid(t, a, gateway, newer)
			}
			syncPaid(t, a, gateway, original)
			results, err := a.deliveryResults(context.Background(), original.ID)
			if err != nil || len(results) != 1 || deliveryText(results[0].Content) != "原策略内容" {
				t.Fatal("策略修改影响原订单交付", err)
			}
			var consumes bool
			if err = a.db.QueryRow("SELECT da.consumes FROM delivery_allocations da JOIN deliveries d ON d.id=da.delivery_id WHERE d.order_id=?", original.ID).Scan(&consumes); err != nil || consumes == initiallyReusable {
				t.Fatal("策略修改篡改消耗快照", consumes, err)
			}
			var sold int
			if err = a.db.QueryRow("SELECT COUNT(*) FROM stock_items WHERE inventory_id=? AND status='sold'", pool.ID).Scan(&sold); err != nil || sold != 1 {
				t.Fatal("旧分配消费边界错误", sold, err)
			}
			if _, _, err = a.reservePricedOrder(context.Background(), orderInput{ProductID: product, Quantity: 1}); err == nil {
				t.Fatal("已售出的一次性条目因策略切换被复活")
			}
		})
	}
}
