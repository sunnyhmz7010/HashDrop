package tinyfaka

import "embed"

// 构建前先运行前端构建；all 保留 Vite 生成的下划线开头资源。
//
//go:embed migrations/*.sql all:backend/web
var Assets embed.FS
