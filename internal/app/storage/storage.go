package storage

import (
	"log"
	"net/http"

	"websql/internal/pkg/appctx"
	"websql/internal/pkg/jsonutil"
	"websql/internal/pkg/lazyinit"
	"websql/internal/pkg/response"

	"github.com/gin-gonic/gin"
)

// 默认实例：lazyinit.Holder 替代散落的 sync.Once + 包级变量模式。
var defaultStorage = &lazyinit.Holder[UserStorageRepo]{}

func getDefaultStorage() UserStorageRepo {
	return defaultStorage.Get(func() UserStorageRepo {
		repo := NewUserStorageRepo(getDB())
		if err := repo.EnsureTable(); err != nil {
			log.Printf("[UserStorage] 自动建表失败: %v", err)
		}
		return repo
	})
}

// List GET /api/storage/list
// 返回当前用户所有 KV 存储条目，前端启动时用于恢复 localStorage。
func List(c *gin.Context) {
	userId := appctx.Ctx.GetUserID(c)
	if userId == "" {
		response.WriteAuthErr(c, "")
		return
	}
	list, err := getDefaultStorage().ListByUserId(userId)
	if err != nil {
		response.WriteErr(c, http.StatusOK, 500, "查询存储失败")
		return
	}
	response.WriteOK(c, list)
}

// Get GET /api/storage/get?key=xxx
func Get(c *gin.Context) {
	userId := appctx.Ctx.GetUserID(c)
	if userId == "" {
		response.WriteAuthErr(c, "")
		return
	}
	key := c.Query("key")
	if key == "" {
		response.WriteErr(c, http.StatusOK, 400, "缺少 key 参数")
		return
	}
	item, err := getDefaultStorage().FindByKey(userId, key)
	if err != nil || item == nil {
		response.WriteOK(c, "")
		return
	}
	response.WriteOK(c, item.StorageValue)
}

// Save POST /api/storage/save  body: {key, value}
func Save(c *gin.Context) {
	userId := appctx.Ctx.GetUserID(c)
	if userId == "" {
		response.WriteAuthErr(c, "")
		return
	}
	req := &StorageSaveRequest{}
	if err := jsonutil.UnmarshalJson(c.Request.Body, req); err != nil {
		response.WriteErr(c, http.StatusOK, 400, "请求参数解析失败")
		return
	}
	if req.Key == "" {
		response.WriteErr(c, http.StatusOK, 400, "key 不能为空")
		return
	}
	if err := getDefaultStorage().Upsert(userId, req.Key, req.Value); err != nil {
		log.Printf("[UserStorage] 保存失败 key=%s: %v", req.Key, err)
		response.WriteErr(c, http.StatusOK, 500, "保存失败")
		return
	}
	response.WriteOK(c, "")
}

// Delete POST /api/storage/delete  body: {key}
func Delete(c *gin.Context) {
	userId := appctx.Ctx.GetUserID(c)
	if userId == "" {
		response.WriteAuthErr(c, "")
		return
	}
	req := &StorageSaveRequest{}
	if err := jsonutil.UnmarshalJson(c.Request.Body, req); err != nil {
		response.WriteErr(c, http.StatusOK, 400, "请求参数解析失败")
		return
	}
	if req.Key == "" {
		response.WriteErr(c, http.StatusOK, 400, "key 不能为空")
		return
	}
	if err := getDefaultStorage().Delete(userId, req.Key); err != nil {
		log.Printf("[UserStorage] 删除失败 key=%s: %v", req.Key, err)
		response.WriteErr(c, http.StatusOK, 500, "删除失败")
		return
	}
	response.WriteOK(c, "")
}
