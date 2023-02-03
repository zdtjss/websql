package store

import (
	"bytes"
	"go-web/logutils"
	"go-web/utils"
	"time"

	"github.com/dablelv/go-huge-util/conv"
)

var store = make(map[string]any, 10)

func StoreItem(key string, val any) {
	if RDB != nil {
		var fval string
		data, err := utils.ToJsonString2(val)
		if err != nil && val != nil {
			fval, err = conv.ToStringE(val)
			logutils.Println(err)
		} else if err == nil {
			fval = string(data)
		}
		err = RDB.Set(ctx, key, fval, 30*time.Minute).Err()
		if err != nil {
			logutils.Printf("key:%s 缓存失败,err:%x", key, err)
		}
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

func GetItem(key string, dist any) {
	if key == "" {
		return
	}
	if RDB != nil {
		val, _ := RDB.Get(ctx, key).Result()
		err := utils.UnmarshalJson2(bytes.NewBufferString(val), &dist)
		if err != nil && val != "" {
			*&dist = val
			RDB.Expire(ctx, key, 30*time.Minute)
		} else if err != nil {
			logutils.Printf("获取key:%s 失败,err:%x", key, err)
		}
	} else {
		v, _ := store[key]
		*&dist = v
	}
}
