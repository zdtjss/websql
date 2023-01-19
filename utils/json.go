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
	if length > 20 {
		w.Header().Add("Content-Encoding", "gzip")
		gw, _ := gzip.NewWriterLevel(w, 1)
		defer gw.Close()
		gw.Write(data)
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
