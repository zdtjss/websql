package storage

// UserStorage 用户级 KV 存储记录。
// CreatedAt/UpdatedAt 使用 *string 而非 *time.Time，
// 因为 modernc.org/sqlite 驱动将 CURRENT_TIMESTAMP 存为文本，无法直接扫描到 time.Time。
type UserStorage struct {
	Id           string  `json:"id" db:"id"`
	UserId       string  `json:"userId" db:"user_id"`
	StorageKey   string  `json:"storageKey" db:"storage_key"`
	StorageValue string  `json:"storageValue" db:"storage_value"`
	CreatedAt    *string `json:"createdAt,omitempty" db:"created_at"`
	UpdatedAt    *string `json:"updatedAt,omitempty" db:"updated_at"`
}

// StorageSaveRequest 保存/删除请求体。
type StorageSaveRequest struct {
	Key   string `json:"key"`
	Value string `json:"value"`
}
