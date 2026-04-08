package agentv2

import (
	"bufio"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"

	"go-web/config"
)

// SessionDB 数据库中的会话记录
type SessionDB struct {
	ID        string    `db:"id"`
	UserID    string    `db:"user_id"`
	Title     string    `db:"title"`
	FilePath  string    `db:"file_path"`
	CreatedAt time.Time `db:"created_at"`
	UpdatedAt time.Time `db:"updated_at"`
}

// CreateSessionInDB 在数据库中创建会话记录
func CreateSessionInDB(id, userID, title, filePath string) error {
	_, err := config.Mngtdb.Exec(`
		INSERT INTO t_ai_session (id, user_id, title, file_path, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?)
	`, id, userID, title, filePath, time.Now(), time.Now())
	if err != nil {
		// 如果表不存在，忽略错误
		if strings.Contains(err.Error(), "doesn't exist") || strings.Contains(err.Error(), "no such table") {
			return nil
		}
	}
	return err
}

// UpdateSessionTitleInDB 更新会话标题
func UpdateSessionTitleInDB(id, title string) error {
	_, err := config.Mngtdb.Exec(`
		UPDATE t_ai_session SET title = ?, updated_at = ? WHERE id = ?
	`, title, time.Now(), id)
	if err != nil {
		// 如果表不存在，忽略错误
		if strings.Contains(err.Error(), "doesn't exist") || strings.Contains(err.Error(), "no such table") {
			return nil
		}
	}
	return err
}

// ListSessionsByUserID 查询用户的会话列表（按创建时间倒序）
func ListSessionsByUserID(userID string) ([]SessionDB, error) {
	var sessions []SessionDB
	err := config.Mngtdb.Select(&sessions, `
		SELECT id, user_id, title, file_path, created_at, updated_at
		FROM t_ai_session
		WHERE user_id = ?
		ORDER BY created_at DESC
	`, userID)
	if err != nil {
		// 如果表不存在，返回空列表
		if strings.Contains(err.Error(), "doesn't exist") || strings.Contains(err.Error(), "no such table") {
			return []SessionDB{}, nil
		}
		return nil, err
	}
	return sessions, nil
}

// GetSessionByID 根据 ID 获取会话
func GetSessionByID(id string) (*SessionDB, error) {
	var session SessionDB
	err := config.Mngtdb.Get(&session, `
		SELECT id, user_id, title, file_path, created_at, updated_at
		FROM t_ai_session
		WHERE id = ?
	`, id)
	if err != nil {
		return nil, err
	}
	return &session, nil
}

// DeleteSessionInDB 删除会话记录
func DeleteSessionInDB(id string) error {
	_, err := config.Mngtdb.Exec(`DELETE FROM t_ai_session WHERE id = ?`, id)
	if err != nil {
		// 如果表不存在，忽略错误
		if strings.Contains(err.Error(), "doesn't exist") || strings.Contains(err.Error(), "no such table") {
			return nil
		}
	}
	return err
}

// SessionExistsInDB 检查会话是否存在于数据库
func SessionExistsInDB(id string) (bool, error) {
	var count int
	err := config.Mngtdb.Get(&count, `SELECT COUNT(*) FROM t_ai_session WHERE id = ?`, id)
	if err != nil {
		log.Printf("[SessionExistsInDB] 查询失败 - id=%s, err=%v\n", id, err)
		// 如果表不存在，返回 false 但不报错
		if strings.Contains(err.Error(), "doesn't exist") || strings.Contains(err.Error(), "no such table") {
			return false, nil
		}
		return false, err
	}
	return count > 0, nil
}

// loadSessionHeader 加载会话文件的 header 信息
func loadSessionHeader(filePath string) (*sessionHeader, error) {
	f, err := os.Open(filePath)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	// 第一行：头部
	if !scanner.Scan() {
		return nil, fmt.Errorf("空的会话文件：%s", filePath)
	}
	var header sessionHeader
	if err := json.Unmarshal(scanner.Bytes(), &header); err != nil {
		return nil, fmt.Errorf("%s 中的会话头部损坏：%w", filePath, err)
	}
	return &header, scanner.Err()
}

// MigrateFileSessionsToDB 将现有的文件会话迁移到数据库（启动时调用）
func MigrateFileSessionsToDB(store *SessionStore) error {
	// 读取 sessions 目录下的所有文件
	entries, err := os.ReadDir(store.dir)
	if err != nil {
		return fmt.Errorf("读取会话目录失败：%w", err)
	}

	migrated := 0
	for _, e := range entries {
		if e.IsDir() || strings.HasPrefix(e.Name(), ".") {
			continue
		}
		if !strings.HasSuffix(e.Name(), ".jsonl") {
			continue
		}

		id := strings.TrimSuffix(e.Name(), ".jsonl")
		if id == "" {
			continue
		}

		// 检查是否已存在于数据库
		exists, err := SessionExistsInDB(id)
		if err != nil {
			continue
		}
		if exists {
			continue
		}

		// 加载 header
		filePath := filepath.Join(store.dir, e.Name())
		header, err := loadSessionHeader(filePath)
		if err != nil {
			continue
		}

		// 插入数据库（使用 id 作为 user_id，因为每个用户只有一个会话）
		err = CreateSessionInDB(header.ID, header.ID, "", filePath)
		if err != nil {
			continue
		}

		migrated++
	}

	if migrated > 0 {
		fmt.Printf("[Migration] 已迁移 %d 个会话到数据库\n", migrated)
	}
	return nil
}
