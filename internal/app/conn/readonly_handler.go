package conn

import (
	"websql/internal/pkg/jsonutil"
	"websql/internal/pkg/response"

	"github.com/gin-gonic/gin"
)

// ReadOnlyStateHandler GET /api/conn/readonly/state?connId=xxx
// 返回连接的只读标记与写解锁剩余时间，供前端展示当前是否可写。
func ReadOnlyStateHandler(c *gin.Context) {
	connID := c.Query("connId")
	if connID == "" {
		response.WriteErr(c, 200, 400, "缺少 connId")
		return
	}
	response.WriteOK(c, gin.H{
		"readOnly":        IsReadOnly(connID),
		"unlockRemaining": WriteUnlockRemainingSecs(connID),
		"allowed":         func() bool { ok, _ := CheckWritable(connID); return ok }(),
	})
}

// UnlockWriteHandler POST /api/conn/readonly/unlock
// 为只读连接开启 60 或 300 秒的写入窗口（仅当前进程有效）。
func UnlockWriteHandler(c *gin.Context) {
	req := struct {
		ConnId       string `json:"connId"`
		DurationSecs int    `json:"durationSecs"`
	}{}
	if err := jsonutil.UnmarshalJson(c.Request.Body, &req); err != nil {
		response.WriteErr(c, 200, 400, "请求参数解析失败")
		return
	}
	secs, err := UnlockWrite(req.ConnId, req.DurationSecs)
	if err != nil {
		response.WriteErr(c, 200, 400, err.Error())
		return
	}
	response.WriteOK(c, gin.H{"unlockRemaining": secs})
}
