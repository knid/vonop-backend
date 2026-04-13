package models

import (
	"encoding/json"
	"fmt"
)

// DataModel represents a database table designed by the AI for the app.
type DataModel struct {
	Name    string       `json:"name"`
	Columns []DataColumn `json:"columns"`
}

// DataColumn represents a column in a DataModel table.
type DataColumn struct {
	Name       string `json:"name"`
	Type       string `json:"type"` // text, integer, boolean, timestamp, uuid, jsonb
	PrimaryKey bool   `json:"primary_key"`
	Nullable   bool   `json:"nullable"`
	Default    string `json:"default,omitempty"`
	References string `json:"references,omitempty"` // e.g. "categories.id"
}

// ParseDataModels unmarshals project.DataModels handling both normal JSON arrays
// and double-encoded strings (where the JSONB column contains `"[{...}]"` instead of `[{...}]`).
func ParseDataModels(raw []byte) ([]DataModel, error) {
	if len(raw) == 0 {
		return nil, nil
	}
	var models []DataModel
	if err := json.Unmarshal(raw, &models); err == nil {
		return models, nil
	}
	// Might be double-encoded: a JSON string containing the array
	var s string
	if err := json.Unmarshal(raw, &s); err == nil && len(s) > 0 {
		if err2 := json.Unmarshal([]byte(s), &models); err2 == nil {
			return models, nil
		}
		return nil, fmt.Errorf("double-encoded data models could not be parsed: %s", s[:min(len(s), 80)])
	}
	return nil, fmt.Errorf("cannot parse data models: %s", string(raw[:min(len(raw), 80)]))
}

// MarshalDataModels validates and marshals data models to JSON.
// Returns nil if models is empty, ensuring only valid []DataModel is stored.
func MarshalDataModels(models []DataModel) ([]byte, error) {
	if len(models) == 0 {
		return nil, nil
	}
	data, err := json.Marshal(models)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal data models: %w", err)
	}
	// Sanity check: must be under 1MB
	if len(data) > 1<<20 {
		return nil, fmt.Errorf("data models too large: %d bytes (max 1MB)", len(data))
	}
	return data, nil
}

// ValidateThemeJSON ensures theme data is a valid JSON object under 50KB.
func ValidateThemeJSON(data []byte) error {
	if len(data) == 0 {
		return nil
	}
	if len(data) > 50*1024 {
		return fmt.Errorf("theme too large: %d bytes (max 50KB)", len(data))
	}
	// Must be a JSON object, not a string or array
	if data[0] != '{' {
		return fmt.Errorf("theme must be a JSON object, got %q", data[:min(len(data), 20)])
	}
	var obj map[string]any
	if err := json.Unmarshal(data, &obj); err != nil {
		return fmt.Errorf("theme is not valid JSON: %w", err)
	}
	return nil
}

// MergeDataModelColumns keeps all auth columns and appends non-conflicting LLM columns.
func MergeDataModelColumns(auth, llm DataModel) DataModel {
	merged := DataModel{Name: auth.Name, Columns: make([]DataColumn, len(auth.Columns))}
	copy(merged.Columns, auth.Columns)

	authCols := make(map[string]bool)
	for _, col := range auth.Columns {
		authCols[col.Name] = true
	}

	for _, col := range llm.Columns {
		if !authCols[col.Name] {
			merged.Columns = append(merged.Columns, col)
		}
	}
	return merged
}
