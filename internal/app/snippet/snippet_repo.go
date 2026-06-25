package snippet

import (
	"bytes"
	"log"
	"strings"

	"github.com/jmoiron/sqlx"
)

// SnippetRepo 定义 SQL 收藏夹数据访问接口，所有针对 t_sql_snippet 的查询均在此实现。
type SnippetRepo interface {
	EnsureTable() error
	List(userId, keyword, category, tag string) ([]*Snippet, error)
	FindById(id string) (*Snippet, error)
	Insert(s *Snippet) error
	Update(s *Snippet) error
	Delete(id, userId string) error
	ListByUserId(userId string) ([]*Snippet, error)
}

type snippetRepo struct {
	db *sqlx.DB
}

// NewSnippetRepo 创建 SnippetRepo 实例。
func NewSnippetRepo(db *sqlx.DB) SnippetRepo {
	return &snippetRepo{db: db}
}

// EnsureTable 在管理库中创建 t_sql_snippet 表（若不存在）。
// 使用 MySQL/SQLite 兼容的 DDL，作为对 init SQL 文件的安全兜底，
// 保证已部署实例在未执行迁移脚本时也能自动建表。
func (r *snippetRepo) EnsureTable() error {
	ddl := `CREATE TABLE IF NOT EXISTS t_sql_snippet (
  id VARCHAR(64) PRIMARY KEY,
  user_id VARCHAR(64),
  title VARCHAR(255) NOT NULL,
  description TEXT,
  sql_content TEXT NOT NULL,
  category VARCHAR(100),
  tags VARCHAR(500),
  db_type VARCHAR(50),
  conn_id VARCHAR(64),
  schema_name VARCHAR(100),
  created_at DATETIME,
  updated_at DATETIME
)`
	if _, err := r.db.Exec(ddl); err != nil {
		return err
	}
	// 创建索引（已存在时忽略错误，兼容 MySQL 与 SQLite）
	r.db.Exec("CREATE INDEX idx_snippet_user_id ON t_sql_snippet(user_id)")
	r.db.Exec("CREATE INDEX idx_snippet_category ON t_sql_snippet(category)")
	return nil
}

// List 按用户、关键字、分类、标签过滤收藏列表。
// keyword 命中 title/description/sql_content；tag 命中 tags 字段（逗号分隔）。
func (r *snippetRepo) List(userId, keyword, category, tag string) ([]*Snippet, error) {
	sql := bytes.Buffer{}
	args := []any{}
	sql.WriteString(`select id, user_id, title, description, sql_content, category, tags, db_type, conn_id, schema_name, created_at, updated_at
 from t_sql_snippet where 1 = 1`)
	if userId != "" {
		sql.WriteString(" and user_id = ?")
		args = append(args, userId)
	}
	if keyword != "" {
		sql.WriteString(" and (title like ? or description like ? or sql_content like ?)")
		kw := "%" + keyword + "%"
		args = append(args, kw, kw, kw)
	}
	if category != "" {
		if category == UncategorizedSentinel {
			// 未分类：category 为空或 NULL
			sql.WriteString(" and (category is null or category = '')")
		} else {
			sql.WriteString(" and category = ?")
			args = append(args, category)
		}
	}
	if tag != "" {
		// tags 为逗号分隔，使用 like 匹配整标签，避免子串误命中
		sql.WriteString(" and (tags = ? or tags like ? or tags like ? or tags like ?)")
		args = append(args, tag, tag+",%", "%,"+tag, "%,"+tag+",%")
	}
	sql.WriteString(" order by updated_at desc, created_at desc")

	list := []*Snippet{}
	if err := r.db.Select(&list, sql.String(), args...); err != nil {
		log.Printf("[SnippetRepo] 查询列表失败: %v", err)
		return nil, err
	}
	return list, nil
}

// FindById 按主键查询单条收藏。
func (r *snippetRepo) FindById(id string) (*Snippet, error) {
	s := &Snippet{}
	err := r.db.Get(s, `select id, user_id, title, description, sql_content, category, tags, db_type, conn_id, schema_name, created_at, updated_at from t_sql_snippet where id = ?`, id)
	if err != nil {
		return nil, err
	}
	return s, nil
}

// Insert 新增一条收藏。
func (r *snippetRepo) Insert(s *Snippet) error {
	_, err := r.db.Exec(`insert into t_sql_snippet (id, user_id, title, description, sql_content, category, tags, db_type, conn_id, schema_name, created_at, updated_at) values (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		s.Id, s.UserId, s.Title, s.Description, s.SqlContent, s.Category, s.Tags, s.DbType, s.ConnId, s.SchemaName, s.CreatedAt, s.UpdatedAt)
	return err
}

// Update 更新一条收藏（不含 user_id 与 created_at）。
func (r *snippetRepo) Update(s *Snippet) error {
	_, err := r.db.Exec(`update t_sql_snippet set title = ?, description = ?, sql_content = ?, category = ?, tags = ?, db_type = ?, conn_id = ?, schema_name = ?, updated_at = ? where id = ?`,
		s.Title, s.Description, s.SqlContent, s.Category, s.Tags, s.DbType, s.ConnId, s.SchemaName, s.UpdatedAt, s.Id)
	return err
}

// Delete 删除指定收藏，限制只能删除本人创建的记录。
func (r *snippetRepo) Delete(id, userId string) error {
	_, err := r.db.Exec("delete from t_sql_snippet where id = ? and user_id = ?", id, userId)
	return err
}

// ListByUserId 查询某用户全部收藏（用于导出）。
func (r *snippetRepo) ListByUserId(userId string) ([]*Snippet, error) {
	list := []*Snippet{}
	err := r.db.Select(&list, `select id, user_id, title, description, sql_content, category, tags, db_type, conn_id, schema_name, created_at, updated_at from t_sql_snippet where user_id = ? order by updated_at desc`, userId)
	if err != nil {
		return nil, err
	}
	return list, nil
}

// splitTags 将逗号分隔的 tags 字符串拆分为切片（去除空白与空项）。
func splitTags(tags string) []string {
	if tags == "" {
		return nil
	}
	parts := strings.Split(tags, ",")
	result := make([]string, 0, len(parts))
	for _, p := range parts {
		t := strings.TrimSpace(p)
		if t != "" {
			result = append(result, t)
		}
	}
	return result
}
