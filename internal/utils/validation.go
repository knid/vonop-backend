package utils

import (
	"errors"
	"fmt"
	"net/http"
	"reflect"
	"strings"

	"github.com/go-playground/validator/v10"
	"github.com/knid/wemob/internal/constants"
)

const tagCustom = "errormgs"

func errorTagFunc[T interface{}](obj interface{}, snp string, fieldname string) error {
	o := obj.(T)

	if !strings.Contains(snp, fieldname) {
		return nil
	}

	fieldArr := strings.Split(snp, ".")
	rsf := reflect.TypeOf(o)

	for i := 1; i < len(fieldArr); i++ {
		field, found := rsf.FieldByName(fieldArr[i])
		if found {
			if fieldArr[i] == fieldname {
				customMessage := field.Tag.Get(tagCustom)
				if customMessage != "" {
					return fmt.Errorf("%s: %s", field.Tag.Get("json"), customMessage)
				}
				return nil
			} else {
				if field.Type.Kind() == reflect.Ptr {
					// If the field type is a pointer, dereference it
					rsf = field.Type.Elem()
				} else {
					rsf = field.Type
				}
			}
		}
	}
	return nil
}

func ValidateFunc[T interface{}](obj interface{}, validate *validator.Validate) (errs error) {
	o := obj.(T)

	defer func() {
		if r := recover(); r != nil {
			errs = fmt.Errorf("can't validate %+v", r)
		}
	}()

	if err := validate.Struct(o); err != nil {
		errorValid := err.(validator.ValidationErrors)
		for _, e := range errorValid {
			// snp  X.Y.Z
			snp := e.StructNamespace()
			errmgs := errorTagFunc[T](obj, snp, e.Field())
			if errmgs != nil {
				errs = errors.Join(errs, fmt.Errorf("%w", errmgs))
			} else {
				errs = errors.Join(errs, fmt.Errorf("%w", e))
			}
		}
	}

	if errs != nil {
		return errs
	}

	return nil
}

// ruleToCode maps a validator tag to the appropriate error code constant.
func ruleToCode(rule string) string {
	switch rule {
	case "required":
		return constants.ErrValidationRequired
	case "email":
		return constants.ErrValidationInvalidEmail
	case "url", "uri":
		return constants.ErrValidationInvalidURL
	case "min":
		return constants.ErrValidationTooShort
	case "max":
		return constants.ErrValidationTooLong
	case "gte", "lte", "gt", "lt", "oneof", "numeric":
		return constants.ErrValidationOutOfRange
	default:
		return constants.ErrValidationInvalidFormat
	}
}

// collectFieldErrors walks an error tree (joined errors / fmt.Errorf %w
// chains) and gathers any embedded validator.FieldError instances. This
// is needed because ValidateFunc joins per-field errors via errors.Join
// before returning, so the raw validator.ValidationErrors slice isn't
// retrievable via a simple type assertion.
func collectFieldErrors(err error) []validator.FieldError {
	if err == nil {
		return nil
	}
	var out []validator.FieldError

	var walk func(e error)
	walk = func(e error) {
		if e == nil {
			return
		}
		if verrs, ok := e.(validator.ValidationErrors); ok {
			for _, fe := range verrs {
				out = append(out, fe)
			}
			return
		}
		if fe, ok := e.(validator.FieldError); ok {
			out = append(out, fe)
			return
		}
		if multi, ok := e.(interface{ Unwrap() []error }); ok {
			for _, sub := range multi.Unwrap() {
				walk(sub)
			}
			return
		}
		if single := errors.Unwrap(e); single != nil {
			walk(single)
		}
	}
	walk(err)
	return out
}

// ValidationError emits a coded validation-error response. For a single
// failing field it picks the most specific VALIDATION_* code and includes
// { field, rule, param } in details. For multiple fields it returns
// VALIDATION_BODY with a details.errors array containing one entry per
// field. If no field-level info can be extracted (e.g. the error came
// from a non-validator source) it falls back to VALIDATION_BODY with the
// caller's raw error string in details.message.
func ValidationError(w http.ResponseWriter, err error) {
	fes := collectFieldErrors(err)
	if len(fes) == 0 {
		msg := constants.DefaultMessages[constants.ErrValidationBody]
		if err != nil {
			msg = err.Error()
		}
		JSONError(w, constants.ErrValidationBody, msg, http.StatusBadRequest, nil)
		return
	}
	if len(fes) == 1 {
		e := fes[0]
		JSONErrorCode(w, ruleToCode(e.Tag()), http.StatusBadRequest, map[string]any{
			"field": e.Field(),
			"rule":  e.Tag(),
			"param": e.Param(),
		})
		return
	}
	entries := make([]map[string]any, 0, len(fes))
	for _, e := range fes {
		entries = append(entries, map[string]any{
			"field": e.Field(),
			"rule":  e.Tag(),
			"param": e.Param(),
			"code":  ruleToCode(e.Tag()),
		})
	}
	JSONError(w, constants.ErrValidationBody, "Validation failed.", http.StatusBadRequest, map[string]any{
		"errors": entries,
	})
}
