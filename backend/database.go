package main

import (
	"context"
	"database/sql"
	"fmt"
	"hashdrop"
	_ "modernc.org/sqlite"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"
)

func openDatabase(path string) (*sql.DB, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return nil, err
	}
	absolute, err := filepath.Abs(path)
	if err != nil {
		return nil, err
	}
	uri := databaseURI(absolute)
	db, err := sql.Open("sqlite", uri+"?_pragma=foreign_keys(1)&_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)&_pragma=synchronous(FULL)&_txlock=immediate")
	if err != nil {
		return nil, err
	}
	// 所有事务串行进入 SQLite；网络请求绝不占用数据库事务。
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	if err = db.Ping(); err != nil {
		db.Close()
		return nil, err
	}
	if err = os.Chmod(absolute, 0600); err != nil {
		db.Close()
		return nil, err
	}
	var version int
	if err = db.QueryRow("PRAGMA user_version").Scan(&version); err != nil {
		db.Close()
		return nil, err
	}
	if version < 0 || version > 3 {
		db.Close()
		return nil, fmt.Errorf("数据库结构版本 %d 不受支持", version)
	}
	if version == 0 {
		if err = initializeDatabase(db); err != nil {
			db.Close()
			return nil, err
		}
	}
	if version == 1 {
		if err = migrateDatabaseV2(db); err != nil {
			db.Close()
			return nil, err
		}
		version = 2
	}
	if version == 2 {
		// 升级前保留可独立恢复的快照，迁移失败时原事务会回滚。
		if err = backupDatabase(context.Background(), db, path+".pre-v3-"+newID()+".db"); err == nil {
			err = migrateDatabaseV3(db)
		}
		if err != nil {
			db.Close()
			return nil, fmt.Errorf("升级交付结构失败: %w", err)
		}
	}
	return db, nil
}

// 按 SQLite 的建新表、复制、替换顺序修改上架约束，保留所有历史外键。
func migrateDatabaseV3(db *sql.DB) (err error) {
	ctx := context.Background()
	conn, err := db.Conn(ctx)
	if err != nil {
		return err
	}
	defer conn.Close()
	if _, err = conn.ExecContext(ctx, "PRAGMA foreign_keys=OFF"); err != nil {
		return err
	}
	defer func() {
		if _, enableErr := conn.ExecContext(ctx, "PRAGMA foreign_keys=ON"); enableErr != nil {
			err = fmt.Errorf("恢复外键约束失败: %w (迁移结果: %v)", enableErr, err)
		}
	}()
	tx, err := conn.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err = tx.ExecContext(ctx, `
CREATE TABLE products_v3 (
 id TEXT PRIMARY KEY, name TEXT NOT NULL, description TEXT NOT NULL DEFAULT '',
 details TEXT NOT NULL DEFAULT '', price INTEGER NOT NULL CHECK(price > 0), currency TEXT NOT NULL,
 inventory_id TEXT REFERENCES inventories(id),
 active INTEGER NOT NULL DEFAULT 0 CHECK(active IN (0,1)),
 hidden INTEGER NOT NULL DEFAULT 0 CHECK(hidden IN (0,1)),
 delivery_mode TEXT NOT NULL DEFAULT 'automatic' CHECK(delivery_mode IN ('automatic','manual')),
 delivery_note TEXT NOT NULL DEFAULT '', buyer_note_label TEXT NOT NULL DEFAULT '',
 buyer_note_required INTEGER NOT NULL DEFAULT 0 CHECK(buyer_note_required IN (0,1)),
 min_quantity INTEGER NOT NULL DEFAULT 1 CHECK(min_quantity BETWEEN 1 AND 100),
 max_quantity INTEGER NOT NULL DEFAULT 100 CHECK(max_quantity BETWEEN min_quantity AND 100),
 created_at INTEGER NOT NULL, updated_at INTEGER NOT NULL, deleted_at INTEGER,
 CHECK(active=0 OR delivery_mode='manual' OR inventory_id IS NOT NULL)
);
INSERT INTO products_v3(id,name,description,details,price,currency,inventory_id,active,hidden,created_at,updated_at,deleted_at)
 SELECT id,name,description,details,price,currency,inventory_id,active,hidden,created_at,updated_at,deleted_at FROM products;
DROP TABLE products;
ALTER TABLE products_v3 RENAME TO products;
CREATE INDEX products_inventory ON products(inventory_id);
ALTER TABLE orders ADD COLUMN delivery_note TEXT NOT NULL DEFAULT '';
ALTER TABLE orders ADD COLUMN buyer_note TEXT NOT NULL DEFAULT '';
CREATE INDEX orders_delivery ON orders(delivery_status,created_at DESC) WHERE deleted_at IS NULL AND payment_status='paid';
PRAGMA user_version=3;`); err != nil {
		return err
	}
	rows, err := tx.QueryContext(ctx, "PRAGMA foreign_key_check")
	if err != nil {
		return err
	}
	broken := rows.Next()
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	if broken {
		return fmt.Errorf("交付结构迁移外键校验失败")
	}
	return tx.Commit()
}

// 旧赞赏保持原凭证和隐私；新增字段默认不加入公开名单。
func migrateDatabaseV2(db *sql.DB) error {
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err = tx.Exec(`
ALTER TABLE donations ADD COLUMN nickname TEXT NOT NULL DEFAULT '';
ALTER TABLE donations ADD COLUMN show_public INTEGER NOT NULL DEFAULT 0 CHECK(show_public IN (0,1));
ALTER TABLE donations ADD COLUMN request_id TEXT NOT NULL DEFAULT '';
CREATE UNIQUE INDEX donations_nickname ON donations(nickname) WHERE nickname <> '';
CREATE UNIQUE INDEX donations_request ON donations(request_id) WHERE request_id <> '';
CREATE INDEX donations_public ON donations(payment_status,show_public,paid_at DESC,id DESC);
PRAGMA user_version = 2;`); err != nil {
		return err
	}
	return tx.Commit()
}

func initializeDatabase(db *sql.DB) error {
	schema, err := hashdrop.Assets.ReadFile("schema.sql")
	if err != nil {
		return err
	}
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err = tx.Exec(string(schema)); err != nil {
		return err
	}
	return tx.Commit()
}

func backupDatabase(ctx context.Context, db *sql.DB, target string) error {
	if err := os.MkdirAll(filepath.Dir(target), 0700); err != nil {
		return err
	}
	if _, err := os.Stat(target); !os.IsNotExist(err) {
		return fmt.Errorf("备份路径必须不存在")
	}
	// VACUUM INTO 创建一致的在线快照，不复制运行中的 WAL 文件。
	if _, err := db.ExecContext(ctx, "VACUUM INTO ?", target); err != nil {
		return err
	}
	if err := os.Chmod(target, 0600); err != nil {
		return err
	}
	absolute, err := filepath.Abs(target)
	if err != nil {
		return err
	}
	check, err := sql.Open("sqlite", databaseURI(absolute)+"?mode=ro")
	if err != nil {
		return err
	}
	defer check.Close()
	var result string
	if err = check.QueryRowContext(ctx, "PRAGMA integrity_check").Scan(&result); err != nil {
		return err
	}
	if result != "ok" {
		return fmt.Errorf("备份完整性校验失败")
	}
	rows, err := check.QueryContext(ctx, "PRAGMA foreign_key_check")
	if err != nil {
		return err
	}
	defer rows.Close()
	if rows.Next() {
		return fmt.Errorf("备份外键校验失败")
	}
	return rows.Err()
}

func unixNow() int64 { return time.Now().Unix() }

func databaseURI(absolute string) string {
	path := filepath.ToSlash(absolute)
	if !strings.HasPrefix(path, "/") {
		path = "/" + path
	}
	return (&url.URL{Scheme: "file", Path: path}).String()
}
