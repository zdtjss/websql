package utils

import (
	"encoding/json"
	"go-web/logutils"
	"io"
	"log"
	"net/http"
	"strconv"
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
	data := ToJsonString(Result{Code: 200, Data: v})
	length := len(data)
	w.Header().Set("Content-Length", strconv.Itoa(length))
	w.Write(data)
}

func UnmarshalJson[T any](r io.Reader, v *T) {
	jsonData, err := io.ReadAll(r)
	logutils.PrintErr(err)
	err = json.Unmarshal(jsonData, v)
	logutils.PrintErr(err)
}

func UnmarshalJson2[T any](r io.Reader, v *T) error {
	jsonData, err := io.ReadAll(r)
	logutils.PrintErr(err)
	return json.Unmarshal(jsonData, v)
}

type Result struct {
	Code uint16 `json:"code"`
	Data any    `json:"data"`
	Msg  any    `json:"msg"`
}
