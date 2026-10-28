package main

import (
	"encoding/json"
	"net/url"
	"strings"
	"unicode/utf8"
)

// 文档描述展示内容；只有商品计划中的 stock 块声明库存需求。
type ContentDocument struct {
	Blocks []ContentBlock `json:"blocks"`
}

type ContentBlock struct {
	ID       string `json:"id"`
	Type     string `json:"type"`
	Text     string `json:"text,omitempty"`
	Label    string `json:"label,omitempty"`
	URL      string `json:"url,omitempty"`
	Quantity int    `json:"quantity,omitempty"`
}

const maxContentBlocks = 1000
const maxContentBytes = 1024 * 1024

func validateContent(doc ContentDocument, allowStock bool) error {
	if len(doc.Blocks) == 0 || len(doc.Blocks) > maxContentBlocks {
		return bad("交付内容须包含 1–1000 个内容块")
	}
	seen := map[string]bool{}
	minimumBlocks := 0
	for _, b := range doc.Blocks {
		minimumBlocks++
		if strings.TrimSpace(b.ID) == "" || !utf8.ValidString(b.ID) || len(b.ID) > 128 || seen[b.ID] {
			return bad("内容块标识为空、重复或过长")
		}
		seen[b.ID] = true
		if !utf8.ValidString(b.Label) || utf8.RuneCountInString(b.Label) > 240 {
			return bad("内容标题不能超过 240 字符")
		}
		switch b.Type {
		case "text":
			if strings.TrimSpace(b.Text) == "" || !utf8.ValidString(b.Text) || utf8.RuneCountInString(b.Text) > 65536 || b.URL != "" || b.Quantity != 0 {
				return bad("文字内容须为 1–65536 字符，且不能包含其他类型字段")
			}
		case "link":
			u, err := url.Parse(b.URL)
			if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Hostname() == "" || u.User != nil || len(b.URL) > 4096 || b.Text != "" || b.Quantity != 0 {
				return bad("链接须为完整的 HTTP 或 HTTPS 地址")
			}
		case "stock":
			if !allowStock || b.Quantity < 0 || b.Quantity > 100 || b.Text != "" || b.URL != "" {
				return bad("取货项仅用于商品计划，每份取货数量须为 1–100")
			}
			minimumBlocks += stockQuantity(b) - 1
		default:
			return bad("不支持的交付内容类型")
		}
	}
	if minimumBlocks > maxContentBlocks {
		return bad("每份商品的固定内容和取货总量不能超过 1000 个内容块")
	}
	encoded, err := encodeContent(doc)
	if err != nil {
		return err
	}
	if len(encoded) > maxContentBytes {
		return bad("一份交付内容不能超过 1 MB")
	}
	return nil
}

func encodeContent(doc ContentDocument) (string, error) {
	value, err := json.Marshal(doc)
	return string(value), err
}

func decodeContent(raw string) (ContentDocument, error) {
	var doc ContentDocument
	if err := json.Unmarshal([]byte(raw), &doc); err != nil {
		return doc, err
	}
	return doc, validateContent(doc, true)
}

func stockQuantity(block ContentBlock) int {
	if block.Quantity == 0 {
		return 1
	}
	return block.Quantity
}
