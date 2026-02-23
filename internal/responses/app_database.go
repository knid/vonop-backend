package responses

type DatabaseSchemaColumn struct {
	Name         string `json:"name"`
	Type         string `json:"type"`
	Nullable     bool   `json:"nullable"`
	IsPrimaryKey bool   `json:"is_primary_key"`
}

type DatabaseSchemaTable struct {
	Name    string                 `json:"name"`
	Columns []DatabaseSchemaColumn `json:"columns"`
}

type DatabaseSchemaResponse struct {
	Tables []DatabaseSchemaTable `json:"tables"`
}

type TableRowsResponse struct {
	Rows       []map[string]any       `json:"rows"`
	TotalCount int                    `json:"total_count"`
	Page       int                    `json:"page"`
	PageSize   int                    `json:"page_size"`
	Columns    []DatabaseSchemaColumn `json:"columns"`
}

type InsertRowResponse struct {
	Success bool           `json:"success"`
	Row     map[string]any `json:"row,omitempty"`
}

type DeleteRowResponse struct {
	Success bool `json:"success"`
}
