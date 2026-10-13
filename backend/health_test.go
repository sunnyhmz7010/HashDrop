package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestContainerHealthCheck(t *testing.T) {
	a, _ := newTestApp(t)
	server := httptest.NewServer(a.routes())
	defer server.Close()
	c := a.config
	c.Listen = strings.TrimPrefix(server.URL, "http://")
	if err := checkHealth(context.Background(), c); err != nil {
		t.Fatal(err)
	}
	for _, status := range []int{http.StatusServiceUnavailable, http.StatusFound} {
		broken := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Location", server.URL+"/api/site")
			w.WriteHeader(status)
		}))
		c.Listen = strings.TrimPrefix(broken.URL, "http://")
		if err := checkHealth(context.Background(), c); err == nil {
			t.Fatalf("健康检查错误接受状态 %d", status)
		}
		broken.Close()
	}
	c.Listen = strings.TrimPrefix(server.URL, "http://")
	server.Close()
	if err := checkHealth(context.Background(), c); err == nil {
		t.Fatal("已停止服务被判定为健康")
	}
}
