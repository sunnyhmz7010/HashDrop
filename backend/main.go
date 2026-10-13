package main

import (
	"bufio"
	"context"
	"database/sql"
	"encoding/base64"
	"flag"
	"fmt"
	"golang.org/x/term"
	"io/fs"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"
	"tinyfaka"
)

type App struct {
	db          *sql.DB
	vault       *vault
	config      Config
	configPath  string
	configMu    sync.RWMutex
	workerMu    sync.Mutex
	authMu      sync.Mutex
	limits      *limiter
	adapters    map[string]APIAdapter
	wake        chan struct{}
	initialized atomic.Bool
	setupMu     sync.Mutex
	setupHash   string
}

func (a *App) settings() Config { a.configMu.RLock(); defer a.configMu.RUnlock(); return a.config }
func main() {
	configPath := flag.String("config", "config.json", "受限配置文件")
	initMode := flag.Bool("init", false, "初始化管理员和密钥，密码从标准输入读取")
	site := flag.String("site-url", "https://shop.example.com", "初始化站点地址")
	dev := flag.Bool("dev", false, "初始化回环开发环境")
	dataDir := flag.String("data-dir", "data", "初始化数据库和备份目录")
	backup := flag.String("backup", "", "在线备份到指定的新文件并退出")
	healthcheck := flag.Bool("healthcheck", false, "检查已运行的回环 HTTP 服务并退出")
	listen := flag.String("listen", "", "覆盖监听地址；默认 127.0.0.1:8080")
	flag.Parse()
	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stdout, nil)))
	if *initMode {
		if err := initialize(*configPath, *site, *dataDir, *dev); err != nil {
			slog.Error("初始化失败", "error", err)
			os.Exit(1)
		}
		return
	}
	c, v, err := readConfig(*configPath)
	uninitialized := os.IsNotExist(err)
	if uninitialized {
		c, err = bootstrapConfig(*dataDir, *listen)
	}
	if err != nil {
		slog.Error("配置读取失败", "error", err)
		os.Exit(1)
	}
	if *listen != "" {
		c.Listen = *listen
		if err = validateListen(c.Listen); err != nil {
			slog.Error("监听地址无效", "error", err)
			os.Exit(1)
		}
	}
	if *healthcheck {
		if err = checkHealth(context.Background(), c); err != nil {
			slog.Error("健康检查失败", "error", err)
			os.Exit(1)
		}
		return
	}
	if *backup != "" {
		if uninitialized {
			slog.Error("尚未初始化，不能备份")
			os.Exit(1)
		}
		if _, err = os.Stat(c.Database); err != nil {
			slog.Error("备份源数据库不存在")
			os.Exit(1)
		}
	}
	var db *sql.DB
	if !uninitialized {
		db, err = openDatabase(c.Database)
		if err != nil {
			slog.Error("数据库打开失败", "error", err)
			os.Exit(1)
		}
	}
	if *backup != "" {
		defer db.Close()
		ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
		defer cancel()
		if err = backupDatabase(ctx, db, *backup); err != nil {
			slog.Error("备份失败", "error", err)
			os.Exit(1)
		}
		slog.Info("备份完成并通过完整性校验")
		return
	}
	app := &App{db: db, vault: v, config: c, configPath: *configPath, limits: newLimiter(), adapters: map[string]APIAdapter{}, wake: make(chan struct{}, 1)}
	app.initialized.Store(!uninitialized)
	defer func() {
		if app.db != nil {
			_ = app.db.Close()
		}
	}()
	if uninitialized {
		if _, err = os.Stat(c.Database); !os.IsNotExist(err) {
			slog.Error("已有数据库但配置缺失，请恢复原配置，不能重新初始化")
			os.Exit(1)
		}
		token := randomToken()
		app.setupHash = tokenHash(token)
		slog.Info("请打开初始化页面并填写一次性初始化凭证", "path", "/setup", "setup_token", token)
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	// 重启后 API 请求可能已产生副作用，恢复必须遵循适配器的幂等契约。
	if !uninitialized {
		if err = app.recoverDeliveries(ctx); err != nil {
			slog.Error("交付恢复失败", "error", err)
			os.Exit(1)
		}
	}
	server := &http.Server{Handler: app.routes(), ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 15 * time.Second, WriteTimeout: 45 * time.Second, IdleTimeout: 60 * time.Second, MaxHeaderBytes: 16 * 1024}
	ln, err := net.Listen("tcp", c.Listen)
	if err != nil {
		slog.Error("监听失败", "error", err)
		os.Exit(1)
	}
	schedulerDone := make(chan struct{})
	go func() { defer close(schedulerDone); app.runScheduler(ctx) }()
	shutdownDone := make(chan struct{})
	go func() {
		defer close(shutdownDone)
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		if e := server.Shutdown(shutdownCtx); e != nil {
			slog.Error("退出失败", "error", e)
		}
	}()
	slog.Info("TinyFaka 启动", "listen", ln.Addr().String())
	if err = server.Serve(ln); err != nil && err != http.ErrServerClosed {
		slog.Error("HTTP 服务失败", "error", err)
		os.Exit(1)
	}
	<-shutdownDone
	<-schedulerDone
}
func initialize(path, site, dataDir string, dev bool) error {
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		return fmt.Errorf("配置文件已存在，拒绝覆盖")
	}
	fmt.Fprintln(os.Stderr, "输入管理员密码（至少 12 字符）：")
	var password string
	if term.IsTerminal(int(os.Stdin.Fd())) {
		b, err := term.ReadPassword(int(os.Stdin.Fd()))
		if err != nil {
			return err
		}
		password = string(b)
		fmt.Fprintln(os.Stderr)
	} else {
		line, err := bufio.NewReader(os.Stdin).ReadString('\n')
		if err != nil && len(line) == 0 {
			return err
		}
		password = strings.TrimRight(line, "\r\n")
	}
	if len(password) < 12 || len(password) > 256 {
		return fmt.Errorf("密码长度须为 12–256 字符")
	}
	key, _ := base64.RawURLEncoding.DecodeString(randomToken())
	dedup, _ := base64.RawURLEncoding.DecodeString(randomToken())
	if dev && site == "https://shop.example.com" {
		site = "http://127.0.0.1:8080"
	}
	absoluteDataDir, err := filepath.Abs(dataDir)
	if err != nil {
		return err
	}
	c := Config{Listen: "127.0.0.1:8080", PublicURL: site, Database: filepath.Join(absoluteDataDir, "tinyfaka.db"), BackupDir: filepath.Join(absoluteDataDir, "backups"), Title: "TinyFaka", Development: dev, AdminPassword: passwordHash(password), CurrentKey: "v1", EncryptionKeys: map[string]string{"v1": base64.StdEncoding.EncodeToString(key)}, DedupKey: base64.StdEncoding.EncodeToString(dedup), HashPay: HashPayConfig{ManualPolicy: "hold"}}
	if err := validateConfig(c); err != nil {
		return err
	}
	if err := writeConfig(path, c); err != nil {
		return err
	}
	fmt.Fprintln(os.Stderr, "初始化完成。生产环境请设置配置文件权限 0600，并配置 HashPay。")
	return nil
}
func (a *App) staticHandler() http.Handler {
	files, _ := fs.Sub(tinyfaka.Assets, "backend/web")
	handler := http.FileServer(http.FS(files))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "GET" && r.Method != "HEAD" {
			http.Error(w, "Method not allowed", 405)
			return
		}
		if strings.HasPrefix(r.URL.Path, "/api/") {
			http.NotFound(w, r)
			return
		}
		if _, err := fs.Stat(files, "index.html"); err != nil {
			http.Error(w, "请先构建前端：npm --prefix frontend run build", 503)
			return
		}
		path := strings.TrimPrefix(r.URL.Path, "/")
		if path == "" {
			path = "index.html"
		}
		if _, err := fs.Stat(files, path); err != nil {
			if strings.Contains(filepath.Base(path), ".") {
				http.NotFound(w, r)
				return
			}
			r = r.Clone(r.Context())
			r.URL.Path = "/"
		}
		if strings.HasPrefix(r.URL.Path, "/assets/") {
			w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
		} else {
			w.Header().Set("Cache-Control", "no-cache")
		}
		handler.ServeHTTP(w, r)
	})
}
