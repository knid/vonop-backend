package utils

import (
	"errors"
	"net/http"
	"strings"
)

func ExtractTokenFromHeader(r *http.Request) (string, error) {
	authHeader := r.Header.Get("Authorization")
	if authHeader == "" {
		return "", errors.New("authorization header is missing")
	}

	splittedAuthHeader := strings.Split(authHeader, "Bearer ")
	if len(splittedAuthHeader) != 2 {
		return "", errors.New("invalid authorization header format")
	}

	tokenString := splittedAuthHeader[1]

	return tokenString, nil
}
