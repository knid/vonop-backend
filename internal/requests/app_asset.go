package requests

import (
	"github.com/go-playground/validator/v10"
	"github.com/knid/wemob/internal/utils"
)

type AssetUploadURLRequest struct {
	Filename    string `json:"filename" validate:"required" errormgs:"filename is required"`
	ContentType string `json:"content_type" validate:"required" errormgs:"content_type is required"`
	FileSize    int64  `json:"file_size" validate:"required,gt=0" errormgs:"file_size must be greater than 0"`
}

func (r *AssetUploadURLRequest) Validate(validate *validator.Validate) error {
	return utils.ValidateFunc[AssetUploadURLRequest](*r, validate)
}

type AssetRenameRequest struct {
	FileName string `json:"file_name" validate:"required" errormgs:"file_name is required"`
}

func (r *AssetRenameRequest) Validate(validate *validator.Validate) error {
	return utils.ValidateFunc[AssetRenameRequest](*r, validate)
}
