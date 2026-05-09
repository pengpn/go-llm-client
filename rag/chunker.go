package rag

import (
	"strings"
	"unicode/utf8"
)

// Chunk 是切片后的文本单元。
type Chunk struct {
	Content  string            // 文本内容
	Meta     map[string]string // 来源信息（文档名、段落序号等）
}

// Chunker 将长文本切分成适合向量化的小块。
type Chunker interface {
	Chunk(text string, meta map[string]string) []Chunk
}

// FixedSizeChunker 按字符数固定切片，带重叠窗口。
//
// 为什么需要重叠（Overlap）？
// 如果一句话恰好跨在两个块的边界，没有重叠时这句话会被截断，
// 导致两个块都无法完整表达这个语义，检索时两块都匹配不上。
// 重叠区域保证边界处的语义不丢失。
type FixedSizeChunker struct {
	chunkSize int // 每块最大字符数
	overlap   int // 相邻块的重叠字符数
}

// NewFixedSizeChunker 创建固定大小切片器。
// 推荐参数：chunkSize=500, overlap=50（约 10% 重叠）。
func NewFixedSizeChunker(chunkSize, overlap int) *FixedSizeChunker {
	if overlap >= chunkSize {
		overlap = chunkSize / 5 // 防止 overlap 过大导致死循环
	}
	return &FixedSizeChunker{
		chunkSize: chunkSize,
		overlap:   overlap,
	}
}

// Chunk 将文本按固定大小切片，返回带 meta 的 Chunk 列表。
func (c *FixedSizeChunker) Chunk(text string, meta map[string]string) []Chunk {
	text = strings.TrimSpace(text)
	if utf8.RuneCountInString(text) == 0 {
		return nil
	}

	runes := []rune(text)
	total := len(runes)
	step := c.chunkSize - c.overlap

	var chunks []Chunk
	for start := 0; start < total; start += step {
		end := start + c.chunkSize
		if end > total {
			end = total
		}

		chunkMeta := make(map[string]string, len(meta)+1)
		for k, v := range meta {
			chunkMeta[k] = v
		}
		chunkMeta["chunk_index"] = itoa(len(chunks))

		chunks = append(chunks, Chunk{
			Content: string(runes[start:end]),
			Meta:    chunkMeta,
		})

		// 已到文档末尾，无需继续
		if end == total {
			break
		}
	}
	return chunks
}

// itoa 简单整数转字符串，避免引入 strconv 包仅为这一个用途。
func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	buf := make([]byte, 0, 10)
	for n > 0 {
		buf = append([]byte{byte('0' + n%10)}, buf...)
		n /= 10
	}
	return string(buf)
}
