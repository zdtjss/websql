package backup

import (
	"database/sql"
	"fmt"
)

type BackupRecord struct {
	Id          string `json:"id" db:"id"`
	Name        string `json:"name" db:"name"`
	ConnId      string `json:"connId" db:"conn_id"`
	Schema      string `json:"schema" db:"schema_name"`
	DbType      string `json:"dbType" db:"db_type"`
	Size        int64  `json:"size" db:"size_bytes"`
	Type        string `json:"type" db:"backup_type"`
	Encrypted   bool   `json:"encrypted" db:"encrypted"`
	CreatedAt   string `json:"createdAt" db:"created_at"`
	Description string `json:"description" db:"description"`
	Status      string `json:"status" db:"status"`
	FilePath    string `json:"filePath" db:"file_path"`
}

type BackupTables struct {
	Table   string `json:"table"`
	Checked bool   `json:"checked"`
}

type BackupCreateRequest struct {
	ConnId      string   `json:"connId"`
	Schema      string   `json:"schema"`
	Name        string   `json:"name"`
	Description string   `json:"description"`
	Tables      []string `json:"tables"`
	WithData    bool     `json:"withData"`
	Encrypt     bool     `json:"encrypt"`
	Compress    bool     `json:"compress"`
}

// anyToString 将数据库驱动返回的各种类型安全转为字符串
// MySQL 驱动可能返回 []byte 或 sql.RawBytes，直接 fmt.Sprintf("%v") 会输出字节数组
func anyToString(v any) string {
	if v == nil {
		return ""
	}
	switch b := v.(type) {
	case []byte:
		return string(b)
	case sql.RawBytes:
		return string(b)
	case string:
		return b
	default:
		return fmt.Sprintf("%v", v)
	}
}

// strScanner 实现 sql.Scanner，强制将任何数据库值转为 string
type strScanner struct {
	Val    string
	HasVal bool
}

func (s *strScanner) Scan(src any) error {
	if src == nil {
		s.Val = ""
		s.HasVal = false
		return nil
	}
	s.HasVal = true
	switch v := src.(type) {
	case []byte:
		s.Val = string(v)
	case sql.RawBytes:
		s.Val = string(v)
	case string:
		s.Val = v
	default:
		s.Val = fmt.Sprintf("%v", v)
	}
	return nil
}
