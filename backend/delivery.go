package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"path/filepath"
	"time"
)

type AdapterConfig struct {
	Adapter    string          `json:"adapter"`
	Parameters json.RawMessage `json:"parameters"`
}
type APIResult struct {
	Content   string
	RetrySafe bool
}

// 适配器由代码注册，固定目标服务与服务端密钥；禁止根据商品配置执行任意 URL 或脚本。
// ReplaySafe 为 true 时必须支持幂等键或在重试前查询既有结果。
type APIAdapter interface {
	Validate(json.RawMessage) error
	ReplaySafe() bool
	Deliver(context.Context, string, json.RawMessage) (APIResult, error)
}
type Delivery struct {
	ID      string `json:"id"`
	Ordinal int    `json:"ordinal"`
	Status  string `json:"status"`
	Content string `json:"content,omitempty"`
	Error   string `json:"error,omitempty"`
}

func (a *App) deliveryResults(ctx context.Context, id string) ([]Delivery, error) {
	rows, err := a.db.QueryContext(ctx, "SELECT id,ordinal,status,result,last_error FROM deliveries WHERE order_id=? ORDER BY ordinal", id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Delivery{}
	for rows.Next() {
		var d Delivery
		var cipher string
		if err = rows.Scan(&d.ID, &d.Ordinal, &d.Status, &cipher, &d.Error); err != nil {
			return nil, err
		}
		if d.Status == "done" {
			d.Content, err = a.vault.decrypt(cipher, "delivery:"+d.ID)
			if err != nil {
				return nil, err
			}
		} else {
			d.Error = ""
		}
		out = append(out, d)
	}
	return out, rows.Err()
}
func (a *App) deliverOrder(ctx context.Context, o Order) error {
	if o.PaymentStatus != "paid" || o.DeliveryStatus == "done" || o.ReservationStatus == "released" {
		return nil
	}
	if o.Kind == "api" {
		return a.deliverAPI(ctx, o)
	}
	tx, err := a.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	rows, err := tx.QueryContext(ctx, "SELECT d.id,COALESCE(d.stock_id,''),COALESCE(s.content,''),COALESCE(s.status,''),COALESCE(s.order_id,'') FROM deliveries d LEFT JOIN stock_items s ON s.id=d.stock_id WHERE d.order_id=? AND d.status='pending' ORDER BY d.ordinal", o.ID)
	if err != nil {
		return err
	}
	type item struct{ id, stock, content, status, order string }
	items := []item{}
	for rows.Next() {
		var i item
		if err = rows.Scan(&i.id, &i.stock, &i.content, &i.status, &i.order); err != nil {
			rows.Close()
			return err
		}
		items = append(items, i)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	for _, i := range items {
		var plain string
		if o.Kind == "static" {
			plain, err = a.vault.decrypt(o.Content, "order:"+o.ID)
		} else {
			if i.status != "reserved" || i.order != o.ID {
				return errors.New("库存预留状态不一致")
			}
			plain, err = a.vault.decrypt(i.content, "stock:"+i.stock)
		}
		if err != nil {
			return err
		}
		encrypted, e := a.vault.encrypt(plain, "delivery:"+i.id)
		if e != nil {
			return e
		}
		if _, err = tx.ExecContext(ctx, "UPDATE deliveries SET result=?,status='done',attempts=attempts+1,last_error='',updated_at=? WHERE id=? AND status='pending'", encrypted, unixNow(), i.id); err != nil {
			return err
		}
		if i.stock != "" {
			result, e := tx.ExecContext(ctx, "UPDATE stock_items SET status='sold' WHERE id=? AND order_id=? AND status='reserved'", i.stock, o.ID)
			if e != nil {
				return e
			}
			n, e := result.RowsAffected()
			if e != nil || n != 1 {
				return errors.New("库存交付冲突")
			}
		}
	}
	if _, err = tx.ExecContext(ctx, "UPDATE orders SET delivery_status='done',reservation_status='consumed',updated_at=? WHERE id=? AND NOT EXISTS(SELECT 1 FROM deliveries WHERE order_id=? AND status<>'done')", unixNow(), o.ID, o.ID); err != nil {
		return err
	}
	if err = tx.Commit(); err != nil {
		return err
	}
	audit("order_delivered", o.ID)
	return nil
}
func (a *App) deliverAPI(ctx context.Context, o Order) error {
	var config AdapterConfig
	if err := json.Unmarshal([]byte(o.Config), &config); err != nil {
		return err
	}
	adapter, exists := a.adapters[config.Adapter]
	if !exists {
		_, err := a.db.ExecContext(ctx, "UPDATE deliveries SET status='manual',last_error='适配器未部署',updated_at=? WHERE order_id=? AND status='pending'", unixNow(), o.ID)
		if err != nil {
			return err
		}
		return a.refreshDeliveryStatus(ctx, o.ID)
	}
	rows, err := a.db.QueryContext(ctx, "SELECT id,idempotency_key,attempts FROM deliveries WHERE order_id=? AND status='pending' AND next_retry<=? ORDER BY ordinal", o.ID, unixNow())
	if err != nil {
		return err
	}
	type item struct {
		id, key  string
		attempts int
	}
	items := []item{}
	for rows.Next() {
		var i item
		if err = rows.Scan(&i.id, &i.key, &i.attempts); err != nil {
			rows.Close()
			return err
		}
		items = append(items, i)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	for _, i := range items {
		result, e := a.db.ExecContext(ctx, "UPDATE deliveries SET status='processing',attempts=attempts+1,updated_at=? WHERE id=? AND status='pending'", unixNow(), i.id)
		if e != nil {
			return e
		}
		n, e := result.RowsAffected()
		if e != nil {
			return e
		}
		if n != 1 {
			continue
		}
		callCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
		res, callErr := adapter.Deliver(callCtx, i.key, config.Parameters)
		cancel()
		status, message, cipher := "done", "", ""
		if callErr != nil {
			status = "manual"
			message = "业务接口结果不明，需要人工核实"
			if (adapter.ReplaySafe() || res.RetrySafe) && i.attempts < 4 {
				status = "pending"
				message = "业务接口暂时失败，等待安全重试"
			}
		} else {
			if res.Content == "" || len(res.Content) > 65536 {
				status = "manual"
				message = "业务接口返回结果无效"
			} else {
				cipher, e = a.vault.encrypt(res.Content, "delivery:"+i.id)
				if e != nil {
					return e
				}
			}
		}
		// 即便请求上下文取消，也落盘记录请求结果；崩溃由恢复路径按幂等契约处理。
		if _, e = a.db.ExecContext(context.Background(), "UPDATE deliveries SET status=?,result=?,last_error=?,next_retry=?,updated_at=? WHERE id=? AND status='processing'", status, cipher, message, unixNow()+int64((i.attempts+1)*60), unixNow(), i.id); e != nil {
			return e
		}
	}
	return a.refreshDeliveryStatus(ctx, o.ID)
}
func (a *App) refreshDeliveryStatus(ctx context.Context, id string) error {
	_, err := a.db.ExecContext(ctx, `UPDATE orders SET delivery_status=CASE WHEN EXISTS(SELECT 1 FROM deliveries WHERE order_id=? AND status='manual') THEN 'manual' WHEN EXISTS(SELECT 1 FROM deliveries WHERE order_id=? AND status<>'done') THEN 'processing' ELSE 'done' END,reservation_status=CASE WHEN NOT EXISTS(SELECT 1 FROM deliveries WHERE order_id=? AND status<>'done') THEN 'consumed' ELSE reservation_status END,updated_at=? WHERE id=?`, id, id, id, unixNow(), id)
	return err
}
func (a *App) recoverDeliveries(ctx context.Context) error {
	rows, err := a.db.QueryContext(ctx, "SELECT d.id,d.order_id,o.config,d.attempts FROM deliveries d JOIN orders o ON o.id=d.order_id WHERE d.status='processing'")
	if err != nil {
		return err
	}
	type item struct {
		id, order, config string
		attempts          int
	}
	items := []item{}
	for rows.Next() {
		var i item
		if err = rows.Scan(&i.id, &i.order, &i.config, &i.attempts); err != nil {
			rows.Close()
			return err
		}
		items = append(items, i)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	for _, i := range items {
		var config AdapterConfig
		if err = json.Unmarshal([]byte(i.config), &config); err != nil {
			return err
		}
		status := "manual"
		if adapter, exists := a.adapters[config.Adapter]; exists && adapter.ReplaySafe() && i.attempts < 5 {
			status = "pending"
		}
		if _, err = a.db.ExecContext(ctx, "UPDATE deliveries SET status=?,last_error='进程中断，按幂等能力恢复',updated_at=? WHERE id=?", status, unixNow(), i.id); err != nil {
			return err
		}
		if err = a.refreshDeliveryStatus(ctx, i.order); err != nil {
			return err
		}
	}
	return nil
}
func (a *App) runScheduler(ctx context.Context) {
	ticker := time.NewTicker(15 * time.Second)
	defer ticker.Stop()
	backupTicker := time.NewTicker(24 * time.Hour)
	defer backupTicker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			a.runPending(ctx)
		case <-a.wake:
			a.runPending(ctx)
		case <-backupTicker.C:
			c := a.settings()
			backupCtx, cancel := context.WithTimeout(ctx, 60*time.Second)
			err := backupDatabase(backupCtx, a.db, filepath.Join(c.BackupDir, fmt.Sprintf("tinyfaka-%d.db", unixNow())))
			cancel()
			if err != nil {
				slog.Error("定期备份失败", "error", err)
			} else {
				slog.Info("定期备份完成并通过校验")
			}
		}
	}
}
func (a *App) runPending(ctx context.Context) {
	rows, err := a.db.QueryContext(ctx, "SELECT id FROM orders WHERE next_check<=? AND (payment_status IN ('creating','pending','review') OR (payment_status='paid' AND delivery_status IN ('pending','processing'))) ORDER BY next_check,created_at LIMIT 20", unixNow())
	if err != nil {
		slog.Error("调度读取失败", "error", err)
		return
	}
	ids := []string{}
	for rows.Next() {
		var id string
		if err = rows.Scan(&id); err != nil {
			rows.Close()
			slog.Error("调度读取失败", "error", err)
			return
		}
		ids = append(ids, id)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		slog.Error("调度读取失败", "error", err)
		return
	}
	for _, id := range ids {
		if ctx.Err() != nil {
			return
		}
		if err = a.syncOrder(ctx, id); err != nil {
			slog.Warn("订单补偿暂未完成", "order_id", id, "reason", safeGatewayError(err))
		}
	}
	if _, err = a.db.ExecContext(ctx, "DELETE FROM admin_sessions WHERE expires_at<=?", unixNow()); err != nil {
		slog.Error("会话清理失败", "error", err)
	}
}
