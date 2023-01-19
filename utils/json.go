package utils

import (
	"compress/gzip"
	"encoding/json"
	"io"
	"log"
	"net/http"
	"strconv"
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
	data := ToJsonString(Result{Code: 200, Data: v})
	length := len(data)
	if length > 4096 {
		w.Header().Add("Content-Encoding", "gzip")
		w2 := gzip.NewWriter(w)
		defer w2.Close()
		w2.Write(data)
	} else {
		w.Header().Add("Content-Length", strconv.Itoa(length))
		w.Write(data)
	}
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
