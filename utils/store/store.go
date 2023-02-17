package store

import (
	"bytes"
	"fmt"
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
			logutils.PrintErr(err)
		} else if err == nil {
			fval = string(data)
		}
		realKey := creakRedisKey(key)
		err = RDB.Set(ctx, realKey, fval, 30*time.Minute).Err()
		logutils.PanicErrf("key:%s 缓存失败", err, realKey)
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
		realKey := creakRedisKey(key)
		val, err := RDB.Get(ctx, realKey).Result()
		logutils.PrintErr(err)
		RDB.Expire(ctx, realKey, 30*time.Minute)
		err = utils.UnmarshalJson2(bytes.NewBufferString(val), &dist)
		if err != nil && val != "" {
			dist = val
		} else if err != nil {
			logutils.PrintErrf("获取key:%s 失败,", err, realKey)
		}
	} else {
		v := store[key]
		utils.UnmarshalJson(bytes.NewBuffer(utils.ToJsonString(v)), &dist)
	}
}

func creakRedisKey(key string) string {
	return fmt.Sprintf("NWAY-WEBSQL-%s", key)
}
