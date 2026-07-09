package storage

import (
	"log"

	"websql/internal/pkg/dberr"
	"websql/internal/pkg/idgen"

	"github.com/jmoiron/sqlx"
)

// UserStorageRepo 用户级 KV 存储数据访问接口。
type UserStorageRepo interface {
	EnsureTable() error
	ListByUserId(userId string) ([]*UserStorage, error)
	FindByKey(userId, key string) (*UserStorage, error)
	Upsert(userId, key, value string) error
	Delete(userId, key string) error
}

type userStorageRepo struct {
	db *sqlx.DB
}

func NewUserStorageRepo(db *sqlx.DB) UserStorageRepo {
	return &userStorageRepo{db: db}
}

// EnsureTable 自动建表，DDL 兼容 MySQL 与 SQLite。
func (r *userStorageRepo) EnsureTable() error {
	ddls := []string{
		`CREATE TABLE IF NOT EXISTS t_user_storage (
  id VARCHAR(36) PRIMARY KEY,
  user_id VARCHAR(36) NOT NULL,
  storage_key VARCHAR(128) NOT NULL,
  storage_value TEXT,
  created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
  updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
)`,
		`CREATE UNIQUE INDEX IF NOT EXISTS idx_user_storage_user_key ON t_user_storage(user_id, storage_key)`,
		`CREATE INDEX IF NOT EXISTS idx_user_storage_user_id ON t_user_storage(user_id)`,
	}
	for _, ddl := range ddls {
		if _, err := r.db.Exec(ddl); err != nil {
			log.Printf("[UserStorage] Exec DDL 失败(可能已存在): %v", err)
		}
	}
	return nil
}

func (r *userStorageRepo) ListByUserId(userId string) ([]*UserStorage, error) {
	list := []*UserStorage{}
	err := r.db.Select(&list,
		`select id, user_id, storage_key, storage_value, created_at, updated_at from t_user_storage where user_id = ? order by storage_key`,
		userId)
	if err != nil {
		log.Printf("[UserStorage] 查询列表失败: %v", err)
		return nil, err
	}
	return list, nil
}

func (r *userStorageRepo) FindByKey(userId, key string) (*UserStorage, error) {
	s := &UserStorage{}
	err := r.db.Get(s,
		`select id, user_id, storage_key, storage_value, created_at, updated_at from t_user_storage where user_id = ? and storage_key = ?`,
		userId, key)
	if err != nil {
		return nil, err
	}
	return s, nil
}

// Upsert 先尝试 INSERT，UNIQUE 冲突时回退为 UPDATE。
// 跳过先 SELECT 的模式，避免扫描时间戳字段可能的驱动兼容问题。
func (r *userStorageRepo) Upsert(userId, key, value string) error {
	_, err := r.db.Exec(
		`insert into t_user_storage (id, user_id, storage_key, storage_value) values (?, ?, ?, ?)`,
		idgen.RandomStr(), userId, key, value)
	if err != nil {
		if !dberr.IsUniqueConstraint(err) {
			log.Printf("[UserStorage] 插入失败 key=%s: %v", key, err)
			return err
		}
		_, err = r.db.Exec(
			`update t_user_storage set storage_value = ?, updated_at = CURRENT_TIMESTAMP where user_id = ? and storage_key = ?`,
			value, userId, key)
		if err != nil {
			log.Printf("[UserStorage] 更新失败 key=%s: %v", key, err)
		}
		return err
	}
	return nil
}

func (r *userStorageRepo) Delete(userId, key string) error {
	_, err := r.db.Exec(
		`delete from t_user_storage where user_id = ? and storage_key = ?`,
		userId, key)
	return err
}
