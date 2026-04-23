package store

import (
	"bytes"
	"go-web/logutils"
	"go-web/utils"
	"sync"
	"time"

	"github.com/duke-git/lancet/v2/convertor"
)

// storeItem 带过期时间的存储项
type storeItem struct {
	value     any
	expiresAt time.Time
}

var (
	store   = make(map[string]*storeItem, 10)
	storeMu sync.RWMutex
)

const defaultTTL = 30 * time.Minute

func init() {
	// 启动后台清理过期 key 的协程
	go func() {
		ticker := time.NewTicker(5 * time.Minute)
		defer ticker.Stop()
		for range ticker.C {
			cleanExpired()
		}
	}()
}

func cleanExpired() {
	storeMu.Lock()
	defer storeMu.Unlock()
	now := time.Now()
	for k, v := range store {
		if now.After(v.expiresAt) {
			delete(store, k)
		}
	}
}

func Add(key string, val any) {
	if RDB != nil {
		fval := convertor.ToString(val)
		err := RDB.Set(ctx, key, fval, defaultTTL).Err()
		logutils.PanicErrf("key:%s 缓存失败", err, key)
	} else {
		storeMu.Lock()
		store[key] = &storeItem{value: val, expiresAt: time.Now().Add(defaultTTL)}
		storeMu.Unlock()
	}
}

func Remove(key string) {
	if RDB != nil {
		RDB.Del(ctx, key)
	} else {
		storeMu.Lock()
		delete(store, key)
		storeMu.Unlock()
	}
}

func Get(key string, dist any) {
	if key == "" {
		return
	}
	if RDB != nil {
		val, err := RDB.Get(ctx, key).Result()
		logutils.PrintErr(err)
		RDB.Expire(ctx, key, defaultTTL)
		err = utils.UnmarshalJson2(bytes.NewBufferString(val), &dist)
		if err != nil && val != "" {
			dist = val
		} else if err != nil {
			logutils.PrintErrf("获取key:%s 失败,", err, key)
		}
	} else {
		storeMu.RLock()
		item, ok := store[key]
		storeMu.RUnlock()
		if !ok || time.Now().After(item.expiresAt) {
			if ok {
				// 已过期，清理
				storeMu.Lock()
				delete(store, key)
				storeMu.Unlock()
			}
			return
		}
		// 续期（滑动过期）
		storeMu.Lock()
		item.expiresAt = time.Now().Add(defaultTTL)
		storeMu.Unlock()
		utils.UnmarshalJson(bytes.NewBuffer(utils.ToJsonString(item.value)), &dist)
	}
}
