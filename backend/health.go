package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"time"
)

// Docker 直接执行同一二进制检查回环 HTTP，不启动数据库或调度器。
func checkHealth(ctx context.Context, c Config) error {
	host, port, err := net.SplitHostPort(c.Listen)
	if err != nil {
		return err
	}
	if host == "0.0.0.0" {
		host = "127.0.0.1"
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, "http://"+net.JoinHostPort(host, port)+"/api/site", nil)
	if err != nil {
		return err
	}
	client := &http.Client{Timeout: 3 * time.Second, CheckRedirect: func(_ *http.Request, _ []*http.Request) error { return http.ErrUseLastResponse }}
	response, err := client.Do(request)
	if err != nil {
		return errors.New("回环 HTTP 服务不可用")
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return fmt.Errorf("回环 HTTP 状态 %d", response.StatusCode)
	}
	var site struct {
		Title string `json:"title"`
	}
	if err = json.NewDecoder(io.LimitReader(response.Body, 64*1024)).Decode(&site); err != nil || site.Title == "" {
		return errors.New("回环 HTTP 响应无效")
	}
	return nil
}
