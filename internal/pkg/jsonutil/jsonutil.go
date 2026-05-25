package jsonutil

import (
	"encoding/json"
	"io"
	"log"
	"net/http"
	"strconv"
	"websql/internal/logger"
)

func ToJsonString(v any) []byte {
	str, err := json.Marshal(v)
	if err != nil {
		log.Panicln(err)
	}
	return str
}

func ToJsonString2(v any) ([]byte, error) {
	str, err := json.Marshal(v)
	if err != nil {
		log.Println(err)
		return []byte{}, err
	}
	return str, nil
}

func WriteJson(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json;charset=UTF-8")
	data, err := json.Marshal(Result{Code: 200, Data: v})
	if err != nil {
		logger.PrintErrf("JSON序列化失败", err)
		errData := []byte(`{"code":500,"msg":"数据序列化失败"}`)
		w.Header().Set("Content-Length", strconv.Itoa(len(errData)))
		w.Write(errData)
		return
	}
	length := len(data)
	w.Header().Set("Content-Length", strconv.Itoa(length))
	w.Write(data)
}

func UnmarshalJson[T any](r io.Reader, v *T) {
	jsonData, err := io.ReadAll(r)
	logger.PrintErr(err)
	err = json.Unmarshal(jsonData, v)
	logger.PrintErr(err)
}

func UnmarshalJson2[T any](r io.Reader, v *T) error {
	jsonData, err := io.ReadAll(r)
	logger.PrintErr(err)
	return json.Unmarshal(jsonData, v)
}

type Result struct {
	Code uint16 `json:"code"`
	Data any    `json:"data"`
	Msg  any    `json:"msg"`
}
