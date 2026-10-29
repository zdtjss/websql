// Package dataset 把大查询结果/上传表格物化为临时 SQLite 数据集。
//
// 目标：模型不必把大量明细读进上下文，而是拿到一个 datasetId，
// 用 dataset_query 在数据集上做聚合、过滤、采样。数据集按用户/会话隔离，
// 带 TTL 与清理；物化前数据已经过权限过滤与脱敏（由调用方保证）。
package dataset

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"websql/internal/database"
	"websql/internal/pkg/idgen"
	"websql/internal/pkg/safego"
	"websql/internal/pkg/sqlguard"

	"github.com/jmoiron/sqlx"
)

const (
	resultTable = "result"
	// defaultQueryLimit dataset_query 默认返回行数
	defaultQueryLimit = 500
	// maxQueryLimit dataset_query 单次最大返回行数（服务端安全边界）
	maxQueryLimit = 2_000
	// maxDatasetRows 单个数据集最大行数（防止磁盘/内存失控）
	maxDatasetRows = 1_000_000
	// maxDatasetsPerSession 单会话数据集数量上限（防止反复物化导致磁盘膨胀）
	maxDatasetsPerSession = 32
)

// Entry 数据集元信息。
type Entry struct {
	ID        string    `json:"id"`
	UserID    string    `json:"-"`
	SessionID string    `json:"-"`
	Path      string    `json:"-"`
	Columns   []string  `json:"columns"`
	RowCount  int       `json:"rowCount"`
	CreatedAt time.Time `json:"createdAt"`
	// Revision 创建数据集时的权限修订号；0 表示文件数据集（无数据库权限语义）。
	Revision uint64 `json:"-"`
}

// QueryResult dataset_query 结果。
type QueryResult struct {
	Columns        []string         `json:"columns"`
	Data           []map[string]any `json:"data,omitempty"`
	ReturnedRows   int              `json:"returnedRows"`
	RowCount       int              `json:"rowCount"`
	EstimatedBytes int              `json:"estimatedBytes,omitempty"`
	Truncated      bool             `json:"truncated,omitempty"`
}

// Store 数据集存储。
type Store struct {
	root    string
	ttl     time.Duration
	mu      sync.Mutex
	entries map[string]*Entry
	stop    chan struct{}
}

var (
	defaultOnce  sync.Once
	defaultStore *Store
)

// Default 返回进程级默认数据集存储。
func Default() *Store {
	defaultOnce.Do(func() {
		root := os.Getenv("AGENT_DATASET_DIR")
		if root == "" {
			cwd, err := os.Getwd()
			if err != nil {
				cwd = os.TempDir()
			}
			// 不放在 agent_workspace 内：那是通用 read_file/ls 的可读根目录，
			// 数据集必须只能经 dataset_query 的归属校验访问。
			root = filepath.Join(cwd, "data", "datasets")
		}
		defaultStore = NewStore(root, time.Hour)
	})
	return defaultStore
}

// NewStore 创建数据集存储并启动过期清理。
func NewStore(root string, ttl time.Duration) *Store {
	if ttl <= 0 {
		ttl = time.Hour
	}
	s := &Store{
		root:    root,
		ttl:     ttl,
		entries: make(map[string]*Entry),
		stop:    make(chan struct{}),
	}
	_ = os.MkdirAll(root, 0o700)
	_ = os.Chmod(root, 0o700)
	s.cleanupOrphans()
	safego.GoWithName("dataset-cleanup", func() {
		ticker := time.NewTicker(5 * time.Minute)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				s.cleanup()
			case <-s.stop:
				return
			}
		}
	})
	return s
}

// Close 停止清理协程（测试/退出时使用）。
func (s *Store) Close() {
	if s == nil {
		return
	}
	select {
	case <-s.stop:
	default:
		close(s.stop)
	}
}

// Create 把脱敏后的结果物化为 SQLite 数据集。
func (s *Store) Create(ctx context.Context, userID, sessionID string, columns []string, rows []map[string]any) (*Entry, error) {
	return s.CreateWithRevision(ctx, userID, sessionID, 0, columns, rows)
}

// CreateWithRevision 创建数据集并记录创建时的权限修订号。
func (s *Store) CreateWithRevision(ctx context.Context, userID, sessionID string, revision uint64, columns []string, rows []map[string]any) (*Entry, error) {
	if s == nil {
		return nil, errors.New("dataset store 未初始化")
	}
	if userID == "" || sessionID == "" {
		return nil, errors.New("缺少用户/会话身份，拒绝创建数据集")
	}
	if len(columns) == 0 {
		return nil, errors.New("结果没有列，无法创建数据集")
	}
	if len(rows) > maxDatasetRows {
		return nil, fmt.Errorf("数据集超过行数上限（%d），请先聚合/过滤", maxDatasetRows)
	}
	s.mu.Lock()
	sessionCount := 0
	for _, entry := range s.entries {
		if entry.UserID == userID && entry.SessionID == sessionID && time.Since(entry.CreatedAt) <= s.ttl {
			sessionCount++
		}
	}
	s.mu.Unlock()
	if sessionCount >= maxDatasetsPerSession {
		return nil, fmt.Errorf("当前会话数据集数量已达上限（%d），请先使用已有数据集或重新聚合", maxDatasetsPerSession)
	}

	normalized := normalizeColumns(columns)
	types := inferColumnTypes(columns, normalized, rows)
	id := idgen.RandomStr()
	dir := filepath.Join(s.root, safeSegment(userID), safeSegment(sessionID))
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, fmt.Errorf("创建数据集目录失败：%w", err)
	}
	_ = os.Chmod(dir, 0o700)
	if hasSymlinkComponent(dir, s.root) {
		return nil, errors.New("数据集目录包含符号链接，拒绝写入")
	}
	finalPath := filepath.Join(dir, id+".sqlite3")
	tmpPath := finalPath + ".tmp"
	_ = os.Remove(tmpPath)

	// 数据集是「一次写入、多次只读」：使用 DELETE journal + FULL synchronous，
	// 关闭连接后主文件即完整，rename 不依赖 WAL checkpoint。
	dsn := "file:" + filepath.ToSlash(tmpPath) + "?_pragma=journal_mode(DELETE)&_pragma=synchronous(FULL)&_pragma=busy_timeout(5000)"
	db, err := sqlx.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("打开数据集失败：%w", err)
	}
	db.SetMaxOpenConns(1)
	defer db.Close()

	ddl := buildDDL(normalized, types)
	if _, err := db.ExecContext(ctx, ddl); err != nil {
		_ = os.Remove(tmpPath)
		return nil, fmt.Errorf("创建数据集表失败：%w", err)
	}

	if len(rows) > 0 {
		tx, err := db.BeginTxx(ctx, nil)
		if err != nil {
			_ = os.Remove(tmpPath)
			return nil, fmt.Errorf("开始数据集事务失败：%w", err)
		}
		insertSQL := buildInsertSQL(normalized)
		stmt, err := tx.PreparexContext(ctx, insertSQL)
		if err != nil {
			_ = tx.Rollback()
			_ = os.Remove(tmpPath)
			return nil, fmt.Errorf("准备数据集写入失败：%w", err)
		}
		for _, row := range rows {
			values := make([]any, len(normalized))
			for i, original := range columnsForInsert(columns, len(normalized)) {
				values[i] = convertValue(row[original], types[i])
			}
			if _, err := stmt.ExecContext(ctx, values...); err != nil {
				_ = stmt.Close()
				_ = tx.Rollback()
				_ = os.Remove(tmpPath)
				return nil, fmt.Errorf("写入数据集失败：%w", err)
			}
		}
		_ = stmt.Close()
		if err := tx.Commit(); err != nil {
			_ = os.Remove(tmpPath)
			return nil, fmt.Errorf("提交数据集失败：%w", err)
		}
	}

	// WAL 文件可能尚未落盘；关闭后 rename 主文件即可，读取时不需要 WAL。
	_ = db.Close()
	if err := os.Rename(tmpPath, finalPath); err != nil {
		_ = os.Remove(tmpPath)
		return nil, fmt.Errorf("完成数据集写入失败：%w", err)
	}
	_ = os.Chmod(finalPath, 0o600)

	entry := &Entry{
		ID:        id,
		UserID:    userID,
		SessionID: sessionID,
		Path:      finalPath,
		Columns:   normalized,
		RowCount:  len(rows),
		CreatedAt: time.Now(),
		Revision:  revision,
	}
	s.mu.Lock()
	s.entries[id] = entry
	s.mu.Unlock()
	return entry, nil
}

// Get 按归属读取数据集元信息。
func (s *Store) Get(id, userID, sessionID string) (*Entry, error) {
	if s == nil {
		return nil, errors.New("dataset store 未初始化")
	}
	s.mu.Lock()
	entry := s.entries[id]
	s.mu.Unlock()
	if entry == nil {
		return nil, errors.New("数据集不存在或已过期")
	}
	if entry.UserID != userID || entry.SessionID != sessionID {
		return nil, errors.New("数据集不属于当前用户/会话")
	}
	if time.Since(entry.CreatedAt) > s.ttl {
		s.remove(id)
		return nil, errors.New("数据集已过期，请重新查询")
	}
	return entry, nil
}

// Query 在数据集上执行只读 SQL。
func (s *Store) Query(ctx context.Context, id, userID, sessionID, sql string, limit int) (*QueryResult, error) {
	return s.QueryWithRevision(ctx, id, userID, sessionID, 0, sql, limit)
}

// QueryWithRevision 在数据集上执行只读 SQL，并校验创建时的权限修订号。
func (s *Store) QueryWithRevision(ctx context.Context, id, userID, sessionID string, revision uint64, sql string, limit int) (*QueryResult, error) {
	entry, err := s.Get(id, userID, sessionID)
	if err != nil {
		return nil, err
	}
	if entry.Revision != 0 && entry.Revision != revision {
		return nil, errors.New("权限已发生变化，数据集已失效，请重新查询")
	}
	if fi, statErr := os.Lstat(entry.Path); statErr != nil || !fi.Mode().IsRegular() || fi.Mode()&os.ModeSymlink != 0 {
		return nil, errors.New("数据集文件不可用，请重新查询")
	}
	if hasSymlinkComponent(entry.Path, s.root) {
		return nil, errors.New("数据集路径包含符号链接，已拒绝查询")
	}
	if err := validateDatasetSQL(sql); err != nil {
		return nil, err
	}
	if limit <= 0 {
		limit = defaultQueryLimit
	}
	if limit > maxQueryLimit {
		limit = maxQueryLimit
	}

	dsn := "file:" + filepath.ToSlash(entry.Path) + "?mode=ro&_pragma=query_only(1)&_pragma=busy_timeout(5000)"
	db, err := sqlx.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("打开数据集失败：%w", err)
	}
	defer db.Close()
	queryCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	rows, err := db.QueryxContext(queryCtx, sql)
	if err != nil {
		return nil, fmt.Errorf("dataset_query 失败：%w", err)
	}
	defer rows.Close()
	cols, _ := rows.Columns()
	data, truncated, err := database.GetResultRowsLimited("sqlite", rows, limit)
	if err != nil {
		return nil, err
	}
	out := &QueryResult{
		Columns:      cols,
		Data:         data,
		ReturnedRows: len(data),
		RowCount:     len(data),
		Truncated:    truncated,
	}
	if encoded, err := json.Marshal(out); err == nil {
		out.EstimatedBytes = len(encoded)
	}
	return out, nil
}

// RemoveSession 删除某会话的全部数据集。
func (s *Store) RemoveSession(userID, sessionID string) {
	if s == nil {
		return
	}
	prefix := filepath.Join(s.root, safeSegment(userID), safeSegment(sessionID))
	s.mu.Lock()
	for id, entry := range s.entries {
		if entry.UserID == userID && entry.SessionID == sessionID {
			delete(s.entries, id)
		}
	}
	s.mu.Unlock()
	_ = os.RemoveAll(prefix)
}

// cleanupOrphans 清理进程重启前遗留的过期数据集文件。
func (s *Store) cleanupOrphans() {
	cutoff := time.Now().Add(-s.ttl)
	_ = filepath.WalkDir(s.root, func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		info, statErr := d.Info()
		if statErr != nil || info.ModTime().After(cutoff) {
			return nil
		}
		_ = os.Remove(path)
		return nil
	})
}

func (s *Store) cleanup() {
	cutoff := time.Now().Add(-s.ttl)
	s.mu.Lock()
	expired := make([]string, 0)
	for id, entry := range s.entries {
		if entry.CreatedAt.Before(cutoff) {
			expired = append(expired, id)
		}
	}
	s.mu.Unlock()
	for _, id := range expired {
		s.remove(id)
	}
}

func (s *Store) remove(id string) {
	s.mu.Lock()
	entry := s.entries[id]
	delete(s.entries, id)
	s.mu.Unlock()
	if entry != nil && entry.Path != "" {
		_ = os.Remove(entry.Path)
		_ = os.Remove(entry.Path + "-wal")
		_ = os.Remove(entry.Path + "-shm")
	}
}

func validateDatasetSQL(sql string) error {
	if strings.TrimSpace(sql) == "" {
		return errors.New("SQL 不能为空")
	}
	if err := sqlguard.ValidateReadOnlyQuery(sql); err != nil {
		return fmt.Errorf("dataset_query 仅允许只读查询：%w", err)
	}
	upper := strings.ToUpper(sqlguard.StripComments(sql))
	for _, bad := range []string{"ATTACH", "DETACH", "PRAGMA", "VACUUM", "REINDEX", "ANALYZE", "LOAD_EXTENSION", "WRITEFILE", "READFILE"} {
		if containsWord(upper, bad) {
			return fmt.Errorf("dataset_query 不允许 %s", bad)
		}
	}
	return nil
}

func containsWord(s, word string) bool {
	idx := 0
	for {
		pos := strings.Index(s[idx:], word)
		if pos < 0 {
			return false
		}
		pos += idx
		beforeOK := pos == 0 || !isWordByte(s[pos-1])
		after := pos + len(word)
		afterOK := after >= len(s) || !isWordByte(s[after])
		if beforeOK && afterOK {
			return true
		}
		idx = pos + len(word)
	}
}

func isWordByte(b byte) bool {
	return b == '_' || (b >= 'a' && b <= 'z') || (b >= 'A' && b <= 'Z') || (b >= '0' && b <= '9')
}

func normalizeColumns(columns []string) []string {
	used := make(map[string]int, len(columns))
	out := make([]string, len(columns))
	for i, col := range columns {
		name := strings.TrimSpace(col)
		if name == "" {
			name = fmt.Sprintf("col_%d", i+1)
		}
		base := name
		lower := strings.ToLower(base)
		if n := used[lower]; n > 0 {
			name = fmt.Sprintf("%s_%d", base, n+1)
			lower = strings.ToLower(name)
		}
		used[lower]++
		out[i] = name
	}
	return out
}

// columnsForInsert 返回与 normalized 一一对应的原始列名。
func columnsForInsert(original []string, n int) []string {
	out := make([]string, n)
	for i := 0; i < n && i < len(original); i++ {
		out[i] = original[i]
	}
	return out
}

func inferColumnTypes(original, normalized []string, rows []map[string]any) []string {
	types := make([]string, len(normalized))
	for i := range normalized {
		typ := "TEXT"
		seenInt, seenFloat, seenText := false, false, false
		limit := len(rows)
		if limit > 2_000 {
			limit = 2_000
		}
		for r := 0; r < limit; r++ {
			value := rows[r][original[i]]
			if value == nil {
				continue
			}
			switch value.(type) {
			case int, int8, int16, int32, int64, uint, uint8, uint16, uint32, uint64, bool:
				seenInt = true
			case float32, float64:
				seenFloat = true
			case string:
				trimmed := strings.TrimSpace(value.(string))
				if trimmed == "" {
					continue
				}
				if _, err := strconv.ParseInt(trimmed, 10, 64); err == nil {
					seenInt = true
				} else if _, err := strconv.ParseFloat(trimmed, 64); err == nil {
					seenFloat = true
				} else {
					seenText = true
				}
			default:
				seenText = true
			}
		}
		switch {
		case seenText:
			typ = "TEXT"
		case seenFloat:
			typ = "REAL"
		case seenInt:
			typ = "INTEGER"
		}
		types[i] = typ
	}
	return types
}

func buildDDL(columns, types []string) string {
	parts := make([]string, 0, len(columns))
	for i, col := range columns {
		parts = append(parts, quoteIdent(col)+" "+types[i])
	}
	return "CREATE TABLE " + quoteIdent(resultTable) + " (" + strings.Join(parts, ", ") + ")"
}

func buildInsertSQL(columns []string) string {
	quoted := make([]string, len(columns))
	placeholders := make([]string, len(columns))
	for i, col := range columns {
		quoted[i] = quoteIdent(col)
		placeholders[i] = "?"
	}
	return "INSERT INTO " + quoteIdent(resultTable) + " (" + strings.Join(quoted, ", ") + ") VALUES (" + strings.Join(placeholders, ", ") + ")"
}

func quoteIdent(name string) string {
	return `"` + strings.ReplaceAll(name, `"`, `""`) + `"`
}

func convertValue(value any, typ string) any {
	if value == nil {
		return nil
	}
	switch typ {
	case "INTEGER":
		switch v := value.(type) {
		case bool:
			if v {
				return int64(1)
			}
			return int64(0)
		case int:
			return int64(v)
		case int8:
			return int64(v)
		case int16:
			return int64(v)
		case int32:
			return int64(v)
		case int64:
			return v
		case uint:
			return int64(v)
		case uint8:
			return int64(v)
		case uint16:
			return int64(v)
		case uint32:
			return int64(v)
		case uint64:
			return int64(v)
		case float32:
			return int64(v)
		case float64:
			return int64(v)
		case string:
			if n, err := strconv.ParseInt(strings.TrimSpace(v), 10, 64); err == nil {
				return n
			}
			return v
		}
	case "REAL":
		switch v := value.(type) {
		case float32:
			return float64(v)
		case float64:
			return v
		case int:
			return float64(v)
		case int8:
			return float64(v)
		case int16:
			return float64(v)
		case int32:
			return float64(v)
		case int64:
			return float64(v)
		case uint:
			return float64(v)
		case uint64:
			return float64(v)
		case string:
			if n, err := strconv.ParseFloat(strings.TrimSpace(v), 64); err == nil {
				return n
			}
			return v
		}
	}
	switch v := value.(type) {
	case time.Time:
		return v.Format(time.RFC3339Nano)
	case []byte:
		return string(v)
	default:
		return fmt.Sprint(v)
	}
}

// hasSymlinkComponent 检查 path（含 root 本身）的路径组件是否存在符号链接。
func hasSymlinkComponent(path, root string) bool {
	absRoot, err := filepath.Abs(root)
	if err != nil {
		return true
	}
	absPath, err := filepath.Abs(path)
	if err != nil {
		return true
	}
	rel, err := filepath.Rel(absRoot, absPath)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return true
	}
	cur := absRoot
	if fi, statErr := os.Lstat(cur); statErr == nil && fi.Mode()&os.ModeSymlink != 0 {
		return true
	}
	for _, part := range strings.Split(rel, string(filepath.Separator)) {
		if part == "" || part == "." {
			continue
		}
		cur = filepath.Join(cur, part)
		fi, statErr := os.Lstat(cur)
		if statErr != nil {
			return false
		}
		if fi.Mode()&os.ModeSymlink != 0 {
			return true
		}
	}
	return false
}

func safeSegment(s string) string {
	if s == "" {
		return "_"
	}
	var b strings.Builder
	changed := false
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z',
			r >= 'A' && r <= 'Z',
			r >= '0' && r <= '9',
			r == '_', r == '-', r == '.':
			b.WriteRune(r)
		default:
			b.WriteByte('_')
			changed = true
		}
	}
	out := strings.Trim(b.String(), ".")
	if out == "" {
		out = "_"
		changed = true
	}
	if changed || len(out) > 64 {
		sum := sha256.Sum256([]byte(s))
		suffix := hex.EncodeToString(sum[:4])
		if len(out) > 48 {
			out = out[:48]
		}
		out = out + "-" + suffix
	}
	return out
}
