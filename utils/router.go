package utils

import "net/http"

var Router map[string]http.HandlerFunc = make(map[string]http.HandlerFunc, 100)

func RegistRouter() {
	for key, value := range Router {
		http.HandleFunc(key, value)
	}
}
