package main

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"path/filepath"
	"strings"
	"time"
	"unicode/utf8"
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
	if o.Kind == "manual" {
		if _, err = tx.ExecContext(ctx, "UPDATE deliveries SET status='manual',last_error='',updated_at=? WHERE order_id=? AND status<>'done'", unixNow(), o.ID); err != nil {
			return err
		}
		if _, err = tx.ExecContext(ctx, "UPDATE orders SET delivery_status='manual',last_error='',updated_at=? WHERE id=?", unixNow(), o.ID); err != nil {
			return err
		}
		return tx.Commit()
	}
	if !validDeliveryKind(o.Kind) && o.Kind != "mixed" {
		return errors.New("订单交付类型无效")
	}
	rows, err := tx.QueryContext(ctx, "SELECT d.id,COALESCE(d.stock_id,''),COALESCE(s.content,''),COALESCE(s.status,''),COALESCE(s.order_id,''),COALESCE(s.inventory_id,''),COALESCE(d.inventory_id,''),d.kind,d.content FROM deliveries d LEFT JOIN stock_items s ON s.id=d.stock_id WHERE d.order_id=? AND d.status='pending' ORDER BY d.ordinal", o.ID)
	if err != nil {
		return err
	}
	type item struct{ id, stock, content, status, order, stockInventory, inventory, kind, snapshot string }
	items := []item{}
	for rows.Next() {
		var i item
		if err = rows.Scan(&i.id, &i.stock, &i.content, &i.status, &i.order, &i.stockInventory, &i.inventory, &i.kind, &i.snapshot); err != nil {
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
		if i.kind == "static" {
			plain, err = a.vault.decrypt(i.snapshot, "delivery-content:"+i.id)
		} else {
			if (i.kind != "card" && i.kind != "account") || i.inventory == "" || i.status != "reserved" || i.order != o.ID || i.stockInventory != i.inventory {
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

// 人工交付一次只完成一份；相同内容重试可恢复结果，已交付内容禁止覆盖。
func (a *App) fulfillDelivery(w http.ResponseWriter, r *http.Request) error {
	var input struct {
		DeliveryID string `json:"delivery_id"`
		Content    string `json:"content"`
	}
	if err := readJSON(w, r, &input); err != nil {
		return err
	}
	if strings.TrimSpace(input.Content) == "" || !utf8.ValidString(input.Content) || utf8.RuneCountInString(input.Content) > 65536 {
		return bad("交付内容须为 1–65536 字符")
	}
	a.workerMu.Lock()
	defer a.workerMu.Unlock()
	tx, err := a.db.BeginTx(r.Context(), nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	o, err := scanOrder(tx.QueryRowContext(r.Context(), orderSelect+" WHERE id=? AND deleted_at IS NULL", r.PathValue("id")))
	if err == sql.ErrNoRows {
		return &apiError{404, "订单不存在"}
	}
	if err != nil {
		return err
	}
	if o.PaymentStatus != "paid" {
		return bad("订单尚未核实付款，不能交付")
	}
	var status, cipher, stockID string
	if err = tx.QueryRowContext(r.Context(), "SELECT status,result,COALESCE(stock_id,'') FROM deliveries WHERE id=? AND order_id=?", input.DeliveryID, o.ID).Scan(&status, &cipher, &stockID); err == sql.ErrNoRows {
		return &apiError{404, "交付项目不存在"}
	} else if err != nil {
		return err
	}
	if status == "done" {
		plain, err := a.vault.decrypt(cipher, "delivery:"+input.DeliveryID)
		if err != nil {
			return err
		}
		if plain != input.Content {
			return &apiError{409, "该份商品已交付，不能覆盖原内容"}
		}
	} else {
		cipher, err = a.vault.encrypt(input.Content, "delivery:"+input.DeliveryID)
		if err != nil {
			return err
		}
		if stockID != "" {
			result, err := tx.ExecContext(r.Context(), "UPDATE stock_items SET status='available',order_id=NULL WHERE id=? AND order_id=? AND status='reserved'", stockID, o.ID)
			if err != nil {
				return err
			}
			count, err := result.RowsAffected()
			if err != nil {
				return err
			}
			if count != 1 {
				return &apiError{409, "库存预留状态不一致，请先核查"}
			}
		}
		if _, err = tx.ExecContext(r.Context(), "UPDATE deliveries SET result=?,status='done',stock_id=NULL,attempts=attempts+1,last_error='',updated_at=? WHERE id=? AND order_id=?", cipher, unixNow(), input.DeliveryID, o.ID); err != nil {
			return err
		}
		if _, err = tx.ExecContext(r.Context(), `UPDATE orders SET delivery_status=CASE WHEN EXISTS(SELECT 1 FROM deliveries WHERE order_id=? AND status<>'done') THEN 'manual' ELSE 'done' END,
 reservation_status=CASE WHEN EXISTS(SELECT 1 FROM deliveries WHERE order_id=? AND status<>'done') THEN reservation_status ELSE 'consumed' END,
 last_error='',updated_at=? WHERE id=?`, o.ID, o.ID, unixNow(), o.ID); err != nil {
			return err
		}
	}
	if err = tx.Commit(); err != nil {
		return err
	}
	audit("delivery_fulfilled", o.ID)
	return a.adminOrder(w, r)
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
			err := backupDatabase(backupCtx, a.db, filepath.Join(c.BackupDir, fmt.Sprintf("hashdrop-%d.db", unixNow())))
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
