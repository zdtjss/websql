package middleware

import (
	"log"
	"runtime/debug"

	"websql/internal/pkg/response"

	"github.com/gin-gonic/gin"
)

func CustomRecovery() gin.HandlerFunc {
	return gin.CustomRecoveryWithWriter(nil, func(c *gin.Context, recovered any) {
		if recovered != nil {

			stack := string(debug.Stack())
			log.Println("PANIC:", recovered)
			log.Println(stack)

			c.Abort()

			if err, ok := recovered.(error); ok {
				response.WriteErr(c, 200, 500, err.Error())
			} else if s, ok := recovered.(string); ok {
				response.WriteErr(c, 200, 500, s)
			} else {
				response.WriteInternalErr(c, "")
			}
		}
	})
}