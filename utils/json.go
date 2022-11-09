package utils

import (
	"encoding/json"
	"log"
)

func ToJsonString(v any) []byte {
	str, err := json.Marshal(v)
	if err != nil {
		log.Println(err)
	}
	return str
}
