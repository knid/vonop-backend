package main

import (
	"encoding/json"
	"fmt"
	"os"
	"sort"

	"github.com/knid/wemob/internal/constants"
)

func main() {
	codes := make([]string, 0, len(constants.DefaultMessages))
	for k := range constants.DefaultMessages {
		codes = append(codes, k)
	}
	sort.Strings(codes)
	out := map[string]any{
		"codes":    codes,
		"defaults": constants.DefaultMessages,
	}
	b, err := json.MarshalIndent(out, "", "  ")
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	if err := os.WriteFile("docs/error-codes.json", b, 0o644); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
