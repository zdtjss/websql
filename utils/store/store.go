package store

import (
	"context"
	"go-web/logutils"
	"time"
)

var store = make(map[string]any, 10)
var ctx = context.Background()

func StoreItem(key string, val any) {
	if RDB != nil {
		RDB.Set(ctx, key, val, 12*time.Hour)
	} else {
		store[key] = val
	}
}

func RemoveItem(key string) {
	if RDB != nil {
		RDB.Del(ctx, key)
	} else {
		delete(store, key)
	}
}

func GetItem(key string) any {
	var v any
	if RDB != nil {
		val, err := RDB.Do(ctx, key).Result()
		logutils.Panicln(err)
		v = val
	} else {
		v, _ = store[key]
	}
	return v
}
