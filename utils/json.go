package utils

import (
	"encoding/json"
	"io"
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
	w.Write(ToJsonString(Result{Code: 200, Data: v}))
}

func UnmarshalJson[T any](r io.Reader, v *T) {
	jsonData, err := io.ReadAll(r)
	Println(err)
	err = json.Unmarshal(jsonData, v)
	Println(err)
}

type Result struct {
	Code uint16 `json:"code"`
	Data any    `json:"data"`
	Msg  any    `json:"msg"`
}
