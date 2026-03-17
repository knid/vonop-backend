package utils

import (
	"fmt"
	"net/http"
	"strings"
)

func ExtractTokenFromHeader(r *http.Request) (string, error) {
	authHeader := r.Header.Get("Authorization")
	if authHeader == "" {
		return "", fmt.Errorf("authorization header is missing")
	}

	splittedAuthHeader := strings.Split(authHeader, "Bearer ")
	if len(splittedAuthHeader) != 2 {
		return "", fmt.Errorf("invalid authorization header format")
	}

	token := strings.TrimSpace(splittedAuthHeader[1])
	if token == "" {
		return "", fmt.Errorf("empty bearer token")
	}

	return token, nil
}
