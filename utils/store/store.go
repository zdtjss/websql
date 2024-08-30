package store

import (
	"bytes"
	"go-web/logutils"
	"go-web/utils"
	"time"

	"github.com/duke-git/lancet/v2/convertor"
)

var store = make(map[string]any, 10)

func Add(key string, val any) {
	if RDB != nil {
		fval := convertor.ToString(val)
		err := RDB.Set(ctx, key, fval, 30*time.Minute).Err()
		logutils.PanicErrf("key:%s 缓存失败", err, key)
	} else {
		store[key] = val
	}
}

func Remove(key string) {
	if RDB != nil {
		RDB.Del(ctx, key)
	} else {
		delete(store, key)
	}
}

func Get(key string, dist any) {
	if key == "" {
		return
	}
	if RDB != nil {
		val, err := RDB.Get(ctx, key).Result()
		logutils.PrintErr(err)
		RDB.Expire(ctx, key, 30*time.Minute)
		err = utils.UnmarshalJson2(bytes.NewBufferString(val), &dist)
		if err != nil && val != "" {
			dist = val
		} else if err != nil {
			logutils.PrintErrf("获取key:%s 失败,", err, key)
		}
	} else {
		v := store[key]
		utils.UnmarshalJson(bytes.NewBuffer(utils.ToJsonString(v)), &dist)
	}
}
