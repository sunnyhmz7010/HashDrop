package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"testing"
)

func TestAdminOrderFiltersAndPagination(t *testing.T) {
	a, _ := newTestApp(t)
	product := insertProduct(t, a, "static", 0)
	for i := 0; i < 25; i++ {
		order, _ := reserve(t, a, product, 1)
		currency, status := "CNY", "paid"
		if i >= 21 {
			currency, status = "USD", "pending"
		}
		if _, err := a.db.Exec("UPDATE orders SET created_at=?,total=?,currency=?,payment_status=? WHERE id=?", 1000+i, 1000+i, currency, status, order.ID); err != nil {
			t.Fatal(err)
		}
	}
	request := adminSessionRequest(t, a)
	for _, tc := range []struct {
		query              string
		total, page, count int
	}{
		{"", 25, 1, 20}, {"page=2", 25, 2, 5}, {"page=99", 25, 2, 5},
		{"currency=CNY&min_amount=10.05&max_amount=10.10", 6, 1, 6},
		{"created_from=1005&created_to=1010", 6, 1, 6},
		{"currency=CNY&min_amount=0.00&max_amount=10.05&status=paid", 6, 1, 6},
		{"currency=USD&status=pending", 4, 1, 4},
		{"currency=CNY&min_amount=20&page=9", 0, 1, 0},
	} {
		t.Run(tc.query, func(t *testing.T) {
			response := request("GET", "/api/admin/orders?"+tc.query)
			if response.Code != http.StatusOK {
				t.Fatal(response.Code, response.Body.String())
			}
			var list struct {
				Items []Order `json:"items"`
				Total int     `json:"total"`
				Page  int     `json:"page"`
				Size  int     `json:"page_size"`
				More  bool    `json:"has_more"`
			}
			if err := json.Unmarshal(response.Body.Bytes(), &list); err != nil {
				t.Fatal(err)
			}
			if list.Total != tc.total || list.Page != tc.page || len(list.Items) != tc.count || list.Size != 20 || list.More != (tc.page*20 < tc.total) {
				t.Fatalf("分页或筛选结果错误: %s", response.Body.String())
			}
		})
	}
	for _, query := range []string{"min_amount=1", "currency=JPY&min_amount=1.1", "currency=CNY&min_amount=2&max_amount=1", "created_from=100&created_to=1", "created_from=abc", "created_from=-1", "currency=CNY&max_amount=NaN", "currency=INVALID", "status=unknown"} {
		t.Run(fmt.Sprintf("invalid_%s", query), func(t *testing.T) {
			response := request("GET", "/api/admin/orders?"+query)
			if response.Code != http.StatusBadRequest {
				t.Fatalf("无效筛选未拒绝: %s %d %s", query, response.Code, response.Body.String())
			}
		})
	}
}
