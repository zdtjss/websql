package storage

import (
	"log"
	"time"

	"websql/internal/database"
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

// EnsureTable 建表由迁移系统（migrations/sqlite/0001_baseline.sql）统一管理，此处保留空实现以兼容接口。
func (r *userStorageRepo) EnsureTable() error {
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

// Upsert 使用单条 INSERT ... ON CONFLICT DO UPDATE（依赖 (user_id, storage_key) 唯一索引）。
// 相比先 INSERT 后回退 UPDATE 的两段式写法，只需一次加锁、一条语句，降低 SQLITE_BUSY 概率。
// 外层仍用 RetryOnBusy 兜底并发写入时的 SQLITE_BUSY。
func (r *userStorageRepo) Upsert(userId, key, value string) error {
	return database.RetryOnBusy(func() error {
		_, err := r.db.Exec(
			`insert into t_user_storage (id, user_id, storage_key, storage_value)
			 values (?, ?, ?, ?)
			 on conflict(user_id, storage_key) do update set
			   storage_value = excluded.storage_value,
			   updated_at = CURRENT_TIMESTAMP`,
			idgen.RandomStr(), userId, key, value)
		if err != nil {
			log.Printf("[UserStorage] 保存失败 key=%s: %v", key, err)
		}
		return err
	}, 5, 50*time.Millisecond)
}

func (r *userStorageRepo) Delete(userId, key string) error {
	return database.RetryOnBusy(func() error {
		_, err := r.db.Exec(
			`delete from t_user_storage where user_id = ? and storage_key = ?`,
			userId, key)
		return err
	}, 5, 50*time.Millisecond)
}
