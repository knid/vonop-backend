package utils

import (
	"encoding/json"
	"log"
	"net/http"
	"reflect"
)

func JSONResponse(w http.ResponseWriter, data interface{}, statusCode int) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(statusCode)
	v := reflect.ValueOf(data)
	if v.Kind() == reflect.Slice && v.IsNil() {
		data = reflect.MakeSlice(v.Type(), 0, 0).Interface()
	}
	json.NewEncoder(w).Encode(data)
}

func JSONErrorResponse(w http.ResponseWriter, message string, code int) {
	log.Println("On JSONErrorResponse, error msg: ", message)
	JSONResponse(w, map[string]string{"error": message}, code)
}
