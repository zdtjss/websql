package utils

import (
	"encoding/json"
	"log"
	"net/http"
)

func ToJsonString(v any) []byte {
	str, err := json.Marshal(v)
	if err != nil {
		log.Println(err)
	}
	return str
}

func WriteJson(w http.ResponseWriter, v any) {
	w.Header().Add("content-type", "application/json;charset=UTF-8")
	w.Write(ToJsonString(v))
}
