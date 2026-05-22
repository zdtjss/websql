package middleware

import (
	"log"
	"net/http"
	"runtime/debug"

	"websql/internal/pkg/sanitize"

	"github.com/gin-gonic/gin"
)

func CustomRecovery() gin.HandlerFunc {
	return gin.CustomRecoveryWithWriter(nil, func(c *gin.Context, recovered any) {
		if recovered != nil {

			stack := string(debug.Stack())
			log.Println("PANIC:", recovered)
			log.Println(stack)

			c.Abort()

			msg := "系统内部错误，请稍后重试"
			if err, ok := recovered.(error); ok {
				msg = sanitize.SanitizeError(err)
			} else if s, ok := recovered.(string); ok {
				msg = sanitize.SanitizeErrMsg(s)
			}

			c.JSON(http.StatusOK, gin.H{
				"code": 500,
				"msg":  msg,
			})
		}
	})
}