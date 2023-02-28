package webapi

import (
	"fmt"
	"log"
	"math"
	"strconv"
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

func TestPainc(t *testing.T) {
	raisePainc()
}

func TestParseInt(t *testing.T) {
	str := "6.30234925680333E+17"
	i, err := strconv.ParseFloat(str, 64)
	if err != nil {
		log.Println(err.Error())
	}
	fmt.Println(int64(i))

	fmt.Printf("%s\n", strconv.FormatUint(math.MaxUint64, 10))
}

func raisePainc() {
	defer func() {
		if err := recover(); err != nil {
			println("aaa")
		}
	}()
	i := 0
	println(1 / i)
}
