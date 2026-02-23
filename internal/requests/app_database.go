package requests

import (
	"github.com/go-playground/validator/v10"
	"github.com/knid/wemob/internal/utils"
)

type InsertRowRequest struct {
	Data map[string]any `json:"data" validate:"required" errormgs:"data is required"`
}

func (r *InsertRowRequest) Validate(validate *validator.Validate) error {
	return utils.ValidateFunc[InsertRowRequest](*r, validate)
}

type UpdateRowRequest struct {
	Column     string         `json:"column" validate:"required" errormgs:"column is required"`
	Value      any            `json:"value"`
	PrimaryKey map[string]any `json:"primary_key" validate:"required" errormgs:"primary key is required"`
}

func (r *UpdateRowRequest) Validate(validate *validator.Validate) error {
	return utils.ValidateFunc[UpdateRowRequest](*r, validate)
}

type DeleteRowRequest struct {
	PrimaryKey map[string]any `json:"primary_key" validate:"required" errormgs:"primary key is required"`
}

func (r *DeleteRowRequest) Validate(validate *validator.Validate) error {
	return utils.ValidateFunc[DeleteRowRequest](*r, validate)
}
