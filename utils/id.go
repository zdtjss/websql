package utils

import (
	"go-web/logutils"

	"github.com/sony/sonyflake"
)

func RandomInt64() uint64 {
	gener := sonyflake.NewSonyflake(sonyflake.Settings{})
	id, err := gener.NextID()
	logutils.Panicln(err)
	return id
}
