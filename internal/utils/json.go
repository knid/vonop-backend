package utils

import (
	"encoding/json"
	"net/http"
)

func ParseJSONBody(r *http.Request, dst interface{}) error {
	decoder := json.NewDecoder(r.Body)
	if err := decoder.Decode(dst); err != nil {
		return err
	}
	return nil
}
