package tinyfaka

import "embed"

// 构建前先运行前端构建；目录占位文件确保后端测试可独立运行。
//
//go:embed migrations/*.sql backend/web
var Assets embed.FS
