package kb

import (
	"strings"

	"clawbench/internal/rag"
)

// DocChunk is a chunk ready to be stored, with the location of the block it
// came from.
type DocChunk struct {
	Text       string
	Segmented  string
	Location   string
	TokenCount int
	Index      int
}

// buildChunks splits extractor blocks into overlapping chunks. A block is never
// merged with another block, so the location label always refers to the passage
// the chunk actually starts in.
func buildChunks(blocks []Block, chunkSize, chunkOverlap int) []DocChunk {
	if chunkSize <= 0 {
		chunkSize = 512
	}
	if chunkOverlap < 0 {
		chunkOverlap = 0
	}
	var out []DocChunk
	idx := 0
	for _, b := range blocks {
		text := strings.TrimSpace(b.Text)
		if text == "" {
			continue
		}
		for _, c := range rag.ChunkText(text, chunkSize, chunkOverlap) {
			out = append(out, DocChunk{
				Text:       c.Text,
				Segmented:  rag.SegmentText(c.Text),
				Location:   b.Location.Label(),
				TokenCount: c.TokenCount,
				Index:      idx,
			})
			idx++
		}
	}
	return out
}
