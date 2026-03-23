package responses

import "time"

type AssetUploadURLResponse struct {
	ID        string `json:"id"`
	UploadURL string `json:"upload_url"`
	PublicURL string `json:"public_url"`
}

type AssetResponse struct {
	ID          string    `json:"id"`
	FileName    string    `json:"file_name"`
	ContentType string    `json:"content_type"`
	FileSize    int64     `json:"file_size"`
	PublicURL   string    `json:"public_url"`
	CreatedAt   time.Time `json:"created_at"`
}
