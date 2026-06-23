package par

import (
	"golang.org/x/sys/cpu"
)

func newBlockCollector[V any](numBlocks int) *blockCollector[V] {
	c := &blockCollector[V]{
		blocks: make([][]V, numBlocks),
	}
	return c
}

type blockCollector[V any] struct {
	_      cpu.CacheLinePad
	blocks [][]V
}

func (c *blockCollector[V]) push(blockId int, v V) {
	c.blocks[blockId] = append(c.blocks[blockId], v)
}

func (c *blockCollector[V]) get(blockId int) []V {
	return c.blocks[blockId]
}

func (c *blockCollector[V]) reset(maxL int) {
	for i := range c.blocks {
		if len(c.blocks[i]) > maxL {
			c.blocks[i] = make([]V, 0)
		} else {
			clear(c.blocks[i])
			c.blocks[i] = c.blocks[i][:0]
		}
	}
}
