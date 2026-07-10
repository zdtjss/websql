package backup

import (
	"github.com/jmoiron/sqlx"
)

// BackupRepo 定义备份数据访问接口，所有针对 t_backup 的 SQL 查询均在此实现
type BackupRepo interface {
	EnsureBackupTable()
	InsertBackupRecord(record *BackupRecord) error
	InsertBackupRecordShort(record *BackupRecord) error
	FindBackups(connId, schema string) ([]BackupRecord, error)
	FindBackupById(id string) (BackupRecord, error)
	DeleteBackupRecord(id string) error
}

type backupRepo struct {
	db *sqlx.DB
}

// NewBackupRepo 创建 BackupRepo 实例，接受 *sqlx.DB 以便未来依赖注入
func NewBackupRepo(db *sqlx.DB) BackupRepo {
	return &backupRepo{db: db}
}

// EnsureBackupTable 建表由迁移系统（migrations/sqlite/0001_baseline.sql）统一管理，
// name 列的增量迁移由 0002_backup_add_name_col.sql 处理，此处保留空实现以兼容接口。
func (r *backupRepo) EnsureBackupTable() {
}

func (r *backupRepo) InsertBackupRecord(record *BackupRecord) error {
	_, err := r.db.Exec(
		"INSERT INTO t_backup (id, name, conn_id, schema_name, db_type, size_bytes, backup_type, encrypted, created_at, description, status, file_path) VALUES (?,?,?,?,?,?,?,?,?,?,?,?)",
		record.Id, record.Name, record.ConnId, record.Schema, record.DbType, record.Size, record.Type, record.Encrypted, record.CreatedAt, record.Description, record.Status, record.FilePath,
	)
	return err
}

func (r *backupRepo) InsertBackupRecordShort(record *BackupRecord) error {
	_, err := r.db.Exec(
		"INSERT INTO t_backup (id, name, conn_id, schema_name, db_type, size_bytes, backup_type, encrypted, created_at, description) VALUES (?,?,?,?,?,?,?,?,?,?)",
		record.Id, record.Name, record.ConnId, record.Schema, record.DbType, record.Size, record.Type, record.Encrypted, record.CreatedAt, record.Description,
	)
	return err
}

func (r *backupRepo) FindBackups(connId, schema string) ([]BackupRecord, error) {
	var records []BackupRecord
	var err error
	if connId != "" && schema != "" {
		err = r.db.Select(&records, "SELECT id,name,conn_id,schema_name,db_type,size_bytes,backup_type,encrypted,created_at,description,COALESCE(status,'completed') status, COALESCE(file_path,'') file_path FROM t_backup WHERE conn_id=? AND schema_name=? ORDER BY created_at DESC", connId, schema)
	} else if connId != "" {
		err = r.db.Select(&records, "SELECT id,name,conn_id,schema_name,db_type,size_bytes,backup_type,encrypted,created_at,description,COALESCE(status,'completed') status, COALESCE(file_path,'') file_path FROM t_backup WHERE conn_id=? ORDER BY created_at DESC", connId)
	} else {
		err = r.db.Select(&records, "SELECT id,name,conn_id,schema_name,db_type,size_bytes,backup_type,encrypted,created_at,description,COALESCE(status,'completed') status, COALESCE(file_path,'') file_path FROM t_backup ORDER BY created_at DESC")
	}
	return records, err
}

func (r *backupRepo) FindBackupById(id string) (BackupRecord, error) {
	var record BackupRecord
	err := r.db.Get(&record, "SELECT * FROM t_backup WHERE id=?", id)
	return record, err
}

func (r *backupRepo) DeleteBackupRecord(id string) error {
	_, err := r.db.Exec("DELETE FROM t_backup WHERE id=?", id)
	return err
}
