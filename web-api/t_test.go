package webapi

import (
	"fmt"
	"testing"
	"time"
)

func TestTimeZone(t *testing.T) {
	now := time.Now()
	nowStr := now.Format("2006-01-02 15:04:05")
	fmt.Println(nowStr)
	nowStr = now.Format(time.RFC3339)
	fmt.Println(nowStr)
}
