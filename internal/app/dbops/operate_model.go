package dbops

import (
	"sync"
	"time"
)

// metaCacheEntry 表元数据缓存条目
type metaCacheEntry struct {
	columnMap   map[string]string
	primaryKeys []string
	expiresAt   time.Time
}

// metaCache 表元数据缓存
type metaCache struct {
	mu      sync.RWMutex
	entries map[string]*metaCacheEntry
}

// tableRawRow 表原始行数据（名称、类型、注释）
type tableRawRow struct {
	Name    string
	Type    string
	Comment string
}

// columnRawRow 列原始行数据（名称、注释）
type columnRawRow struct {
	Name    string
	Comment string
}
