package store

import (
	"context"
	"time"
)

var store map[string]any
var ctx = context.Background()

func StoreItem(key string, val any) {
	if RDB != nil {
		RDB.Set(ctx, key, val, 12*time.Hour)
	} else {
		store[key] = val
	}
}

func GetItem(key string, val any) {
	if RDB != nil {
		RDB.Get(ctx, key)
	} else {
		store[key] = val
	}
}
