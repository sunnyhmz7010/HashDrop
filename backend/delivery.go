package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"path/filepath"
	"time"
)

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
	tx, err := a.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	// 历史订单保留领取记录；已停止支持的类型不能进入库存分配。
	if !validProductKind(o.Kind) {
		if _, err = tx.ExecContext(ctx, "UPDATE deliveries SET status='manual',last_error='商品交付类型已停止支持',updated_at=? WHERE order_id=? AND status<>'done'", unixNow(), o.ID); err != nil {
			return err
		}
		if _, err = tx.ExecContext(ctx, "UPDATE orders SET delivery_status='manual',last_error='商品交付类型已停止支持',updated_at=? WHERE id=?", unixNow(), o.ID); err != nil {
			return err
		}
		return tx.Commit()
	}
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
			if !a.initialized.Load() {
				continue
			}
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
	if !a.initialized.Load() {
		return
	}
	rows, err := a.db.QueryContext(ctx, "SELECT id FROM orders WHERE next_check<=? AND (payment_status IN ('creating','pending','expired','review') OR (payment_status='paid' AND delivery_status IN ('pending','processing'))) ORDER BY next_check,created_at LIMIT 20", unixNow())
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
	a.runPendingDonations(ctx)
	if _, err = a.db.ExecContext(ctx, "DELETE FROM admin_sessions WHERE expires_at<=?", unixNow()); err != nil {
		slog.Error("会话清理失败", "error", err)
	}
}
