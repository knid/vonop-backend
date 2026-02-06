package requests

import (
	"github.com/go-playground/validator/v10"
	"github.com/knid/wemob/internal/utils"
)

type FileCreateRequest struct {
	FilePath string `json:"file_path" validate:"required" errormgs:"is required"`
	Content  string `json:"content" validate:"required" errormgs:"is required"`
	FileType string `json:"file_type" validate:"required,oneof=html css js json" errormgs:"must be html, css, js, or json"`
}

func (r *FileCreateRequest) Validate(validate *validator.Validate) error {
	return utils.ValidateFunc[FileCreateRequest](*r, validate)
}

type FileUpdateRequest struct {
	Content string `json:"content" validate:"required" errormgs:"is required"`
}

func (r *FileUpdateRequest) Validate(validate *validator.Validate) error {
	return utils.ValidateFunc[FileUpdateRequest](*r, validate)
}

type ChatMessageRequest struct {
	Message string `json:"message" validate:"required,min=2" errormgs:"must be at least 2 characters long"`
}

func (r *ChatMessageRequest) Validate(validate *validator.Validate) error {
	return utils.ValidateFunc[ChatMessageRequest](*r, validate)
}
