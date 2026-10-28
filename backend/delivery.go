package main

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"path/filepath"
	"time"
)

type Delivery struct {
	ID      string           `json:"id"`
	Ordinal int              `json:"ordinal"`
	Status  string           `json:"status"`
	Content *ContentDocument `json:"content,omitempty"`
	Error   string           `json:"error,omitempty"`
}

var errDeliveryReservation = errors.New("库存预留状态不一致，请核查后重试或补交")
var errDeliverySnapshot = errors.New("交付快照无法读取，请核查密钥或人工补交")

func (a *App) deliveryMetadata(ctx context.Context, orderID string) ([]Delivery, error) {
	rows, err := a.db.QueryContext(ctx, "SELECT id,ordinal,status FROM deliveries WHERE order_id=? ORDER BY ordinal", orderID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Delivery{}
	for rows.Next() {
		var d Delivery
		if err = rows.Scan(&d.ID, &d.Ordinal, &d.Status); err != nil {
			return nil, err
		}
		out = append(out, d)
	}
	return out, rows.Err()
}

// 管理详情读取结果；买家状态查询不读取交付正文。
func (a *App) deliveryResults(ctx context.Context, orderID string) ([]Delivery, error) {
	rows, err := a.db.QueryContext(ctx, "SELECT id,ordinal,status,result,last_error FROM deliveries WHERE order_id=? ORDER BY ordinal", orderID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Delivery{}
	for rows.Next() {
		var d Delivery
		var encrypted string
		if err = rows.Scan(&d.ID, &d.Ordinal, &d.Status, &encrypted, &d.Error); err != nil {
			return nil, err
		}
		if d.Status == "done" {
			d.Content, err = a.readDeliveryContent(d.ID, encrypted)
			if err != nil {
				return nil, err
			}
		}
		out = append(out, d)
	}
	return out, rows.Err()
}

func (a *App) readDeliveryContent(id, encrypted string) (*ContentDocument, error) {
	plain, err := a.vault.decrypt(encrypted, "delivery:"+id)
	if err != nil {
		return nil, errDeliverySnapshot
	}
	doc, err := decodeContent(plain)
	if err != nil || validateContent(doc, false) != nil {
		return nil, errDeliverySnapshot
	}
	return &doc, nil
}

// 领取授权独立处理，身份和保护策略无需进入库存分配器。
func (a *App) authorizeDeliveryAccess(r *http.Request) (Order, Delivery, error) {
	o, err := a.authorizeOrderAccess(r)
	if err != nil {
		return Order{}, Delivery{}, err
	}
	var d Delivery
	var encrypted string
	err = a.db.QueryRowContext(r.Context(), "SELECT id,ordinal,status,result FROM deliveries WHERE id=? AND order_id=?", r.PathValue("delivery"), o.ID).Scan(&d.ID, &d.Ordinal, &d.Status, &encrypted)
	if err == sql.ErrNoRows {
		return Order{}, Delivery{}, &apiError{404, "交付内容不存在"}
	}
	if err != nil {
		return Order{}, Delivery{}, err
	}
	if o.PaymentStatus != "paid" || d.Status != "done" {
		return Order{}, Delivery{}, &apiError{409, "交付内容尚未准备好"}
	}
	d.Content, err = a.readDeliveryContent(d.ID, encrypted)
	return o, d, err
}

func (a *App) claimDelivery(w http.ResponseWriter, r *http.Request) error {
	var input struct{}
	if err := readJSON(w, r, &input); err != nil {
		return err
	}
	_, d, err := a.authorizeDeliveryAccess(r)
	if err != nil {
		return err
	}
	audit("delivery_claimed", d.ID)
	reply(w, 200, map[string]any{"delivery": d})
	return nil
}

func refreshDeliverySummary(ctx context.Context, tx *sql.Tx, orderID string) error {
	var total, done, failed, cancelled int
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(*),COALESCE(SUM(status='done'),0),COALESCE(SUM(status='failed'),0),COALESCE(SUM(status='cancelled'),0) FROM deliveries WHERE order_id=?`, orderID).Scan(&total, &done, &failed, &cancelled); err != nil {
		return err
	}
	status := "pending"
	switch {
	case total > 0 && done == total:
		status = "done"
	case done > 0:
		status = "partial"
	case failed > 0:
		status = "failed"
	case total > 0 && cancelled == total:
		status = "cancelled"
	}
	_, err := tx.ExecContext(ctx, `UPDATE orders SET delivery_status=?,reservation_status=CASE WHEN ?='done' THEN 'consumed' ELSE reservation_status END,updated_at=? WHERE id=?`, status, status, unixNow(), orderID)
	return err
}

func (a *App) deliverOrder(ctx context.Context, o Order) error {
	rows, err := a.db.QueryContext(ctx, `SELECT d.id FROM deliveries d JOIN orders o ON o.id=d.order_id WHERE o.id=? AND o.payment_status='paid' AND d.status='pending' ORDER BY d.ordinal`, o.ID)
	if err != nil {
		return err
	}
	ids := []string{}
	for rows.Next() {
		var id string
		if err = rows.Scan(&id); err != nil {
			rows.Close()
			return err
		}
		ids = append(ids, id)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	var firstError error
	for _, id := range ids {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if err = a.deliverItem(ctx, o.ID, id); err == nil {
			continue
		}
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if firstError == nil {
			firstError = err
		}
		reason := "交付处理失败，请稍后重试"
		if errors.Is(err, errDeliveryReservation) || errors.Is(err, errDeliverySnapshot) {
			reason = err.Error()
		}
		tx, txErr := a.db.BeginTx(ctx, nil)
		if txErr != nil {
			return txErr
		}
		_, txErr = tx.ExecContext(ctx, "UPDATE deliveries SET status='failed',attempts=attempts+1,last_error=?,updated_at=? WHERE id=? AND status='pending'", reason, unixNow(), id)
		if txErr == nil {
			txErr = refreshDeliverySummary(ctx, tx, o.ID)
		}
		if txErr == nil {
			txErr = tx.Commit()
		} else {
			tx.Rollback()
		}
		if txErr != nil {
			return txErr
		}
		slog.Warn("交付项处理失败", "delivery_id", id, "reason", reason)
	}
	return firstError
}

func (a *App) deliverItem(ctx context.Context, orderID, deliveryID string) error {
	tx, err := a.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var payment, reservation, status, encrypted string
	if err = tx.QueryRowContext(ctx, `SELECT o.payment_status,o.reservation_status,d.status,d.content FROM orders o JOIN deliveries d ON d.order_id=o.id WHERE o.id=? AND d.id=?`, orderID, deliveryID).Scan(&payment, &reservation, &status, &encrypted); err != nil {
		return err
	}
	if status != "pending" {
		return nil
	}
	if payment != "paid" || reservation == "released" {
		return errDeliveryReservation
	}
	plain, err := a.vault.decrypt(encrypted, "delivery-content:"+deliveryID)
	if err != nil {
		return errDeliverySnapshot
	}
	doc, err := decodeContent(plain)
	if err != nil || validateContent(doc, false) != nil {
		return errDeliverySnapshot
	}
	rows, err := tx.QueryContext(ctx, `SELECT stock_id,inventory_id,consumes,status FROM delivery_allocations WHERE delivery_id=? ORDER BY ordinal`, deliveryID)
	if err != nil {
		return err
	}
	type resource struct {
		stock, inventory, status string
		consumes                 bool
	}
	resources := []resource{}
	for rows.Next() {
		var v resource
		if err = rows.Scan(&v.stock, &v.inventory, &v.consumes, &v.status); err != nil {
			rows.Close()
			return err
		}
		resources = append(resources, v)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	for _, v := range resources {
		if v.status != "reserved" {
			return errDeliveryReservation
		}
		if !v.consumes {
			continue
		}
		result, e := tx.ExecContext(ctx, "UPDATE stock_items SET status='sold' WHERE id=? AND inventory_id=? AND order_id=? AND status='reserved'", v.stock, v.inventory, orderID)
		if e != nil {
			return e
		}
		count, e := result.RowsAffected()
		if e != nil {
			return e
		}
		if count != 1 {
			return errDeliveryReservation
		}
	}
	result, err := a.vault.encrypt(plain, "delivery:"+deliveryID)
	if err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, "UPDATE delivery_allocations SET status='consumed' WHERE delivery_id=? AND status='reserved'", deliveryID); err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, "UPDATE deliveries SET result=?,status='done',attempts=attempts+1,last_error='',updated_at=? WHERE id=? AND status='pending'", result, unixNow(), deliveryID); err != nil {
		return err
	}
	if err = refreshDeliverySummary(ctx, tx, orderID); err != nil {
		return err
	}
	if err = tx.Commit(); err != nil {
		return err
	}
	audit("delivery_completed", deliveryID)
	return nil
}

// 只释放本订单的未消耗分配，共享文件和其他订单的预留不受影响。
func releaseDeliveryAllocations(ctx context.Context, tx *sql.Tx, orderID, deliveryID string) error {
	rows, err := tx.QueryContext(ctx, "SELECT stock_id FROM delivery_allocations WHERE delivery_id=? AND consumes=1 AND status='reserved'", deliveryID)
	if err != nil {
		return err
	}
	ids := []string{}
	for rows.Next() {
		var id string
		if err = rows.Scan(&id); err != nil {
			rows.Close()
			return err
		}
		ids = append(ids, id)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	for _, id := range ids {
		result, e := tx.ExecContext(ctx, "UPDATE stock_items SET status='available',order_id=NULL WHERE id=? AND order_id=? AND status='reserved'", id, orderID)
		if e != nil {
			return e
		}
		n, e := result.RowsAffected()
		if e != nil {
			return e
		}
		if n != 1 {
			return errDeliveryReservation
		}
	}
	_, err = tx.ExecContext(ctx, "UPDATE delivery_allocations SET status='released' WHERE delivery_id=? AND status='reserved'", deliveryID)
	return err
}

func (a *App) fulfillDelivery(w http.ResponseWriter, r *http.Request) error {
	var input struct {
		DeliveryID string          `json:"delivery_id"`
		Content    ContentDocument `json:"content"`
	}
	if err := readJSON(w, r, &input); err != nil {
		return err
	}
	if err := validateContent(input.Content, false); err != nil {
		return err
	}
	plain, err := encodeContent(input.Content)
	if err != nil {
		return err
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
	var status, encrypted string
	err = tx.QueryRowContext(r.Context(), "SELECT status,result FROM deliveries WHERE id=? AND order_id=?", input.DeliveryID, o.ID).Scan(&status, &encrypted)
	if err == sql.ErrNoRows {
		return &apiError{404, "交付项目不存在"}
	}
	if err != nil {
		return err
	}
	if status == "done" {
		previous, err := a.vault.decrypt(encrypted, "delivery:"+input.DeliveryID)
		if err != nil {
			return err
		}
		if previous != plain {
			return &apiError{409, "该份商品已交付，不能覆盖原内容"}
		}
	} else {
		if err = releaseDeliveryAllocations(r.Context(), tx, o.ID, input.DeliveryID); err != nil {
			return err
		}
		encrypted, err = a.vault.encrypt(plain, "delivery:"+input.DeliveryID)
		if err != nil {
			return err
		}
		if _, err = tx.ExecContext(r.Context(), "UPDATE deliveries SET result=?,status='done',attempts=attempts+1,last_error='',updated_at=? WHERE id=?", encrypted, unixNow(), input.DeliveryID); err != nil {
			return err
		}
		if err = refreshDeliverySummary(r.Context(), tx, o.ID); err != nil {
			return err
		}
	}
	if err = tx.Commit(); err != nil {
		return err
	}
	audit("delivery_fulfilled", input.DeliveryID)
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
			backupCtx, cancel := context.WithTimeout(ctx, 30*time.Minute)
			err := backupDatabase(backupCtx, a.db, filepath.Join(a.settings().BackupDir, fmt.Sprintf("hashdrop-%d.db", unixNow())))
			cancel()
			if err != nil {
				slog.Error("定期备份失败", "error_type", fmt.Sprintf("%T", err))
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
	rows, err := a.db.QueryContext(ctx, `SELECT id FROM orders WHERE next_check<=? AND payment_status IN ('creating','pending','expired','review') ORDER BY next_check,created_at LIMIT 20`, unixNow())
	if err != nil {
		slog.Error("支付调度读取失败", "error_type", fmt.Sprintf("%T", err))
		return
	}
	ids := []string{}
	for rows.Next() {
		var id string
		if err = rows.Scan(&id); err != nil {
			break
		}
		ids = append(ids, id)
	}
	if err == nil {
		err = rows.Err()
	}
	rows.Close()
	if err != nil {
		slog.Error("支付调度读取失败", "error_type", fmt.Sprintf("%T", err))
		return
	}
	for _, id := range ids {
		if ctx.Err() != nil {
			return
		}
		if err = a.syncOrder(ctx, id); err != nil {
			slog.Warn("支付补查暂未完成", "order_id", id, "reason", safeGatewayError(err))
		}
	}
	// 已付款的交付独立读取本地快照，不受网关故障或支付查询退避影响。
	rows, err = a.db.QueryContext(ctx, `SELECT o.id FROM orders o WHERE o.payment_status='paid' AND EXISTS(SELECT 1 FROM deliveries d WHERE d.order_id=o.id AND d.status='pending') ORDER BY o.created_at LIMIT 20`)
	if err != nil {
		slog.Error("交付调度读取失败", "error_type", fmt.Sprintf("%T", err))
		return
	}
	ids = ids[:0]
	for rows.Next() {
		var id string
		if err = rows.Scan(&id); err != nil {
			break
		}
		ids = append(ids, id)
	}
	if err == nil {
		err = rows.Err()
	}
	rows.Close()
	if err != nil {
		slog.Error("交付调度读取失败", "error_type", fmt.Sprintf("%T", err))
		return
	}
	for _, id := range ids {
		if ctx.Err() != nil {
			return
		}
		a.workerMu.Lock()
		err = a.deliverOrder(ctx, Order{ID: id})
		a.workerMu.Unlock()
		if err != nil {
			slog.Warn("交付等待处理", "order_id", id)
		}
	}
	a.runPendingDonations(ctx)
	if _, err = a.db.ExecContext(ctx, "DELETE FROM admin_sessions WHERE expires_at<=?", unixNow()); err != nil {
		slog.Error("会话清理失败", "error_type", fmt.Sprintf("%T", err))
	}
}
