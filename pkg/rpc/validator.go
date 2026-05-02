package rpc

import (
	"net/http"

	"github.com/vmkteam/zenrpc/v2"
)

// Field/error wiring for RPC validation.
const (
	FieldErrorRequired = "required"
	FieldErrorMax      = "max"
	FieldErrorMin      = "min"
	FieldErrorUnique   = "unique"
	FieldErrorFormat   = "format"
	FieldErrorLen      = "len"
)

type FieldError struct {
	Field      string                `json:"field"`
	Error      string                `json:"error"`
	Constraint *FieldErrorConstraint `json:"constraint,omitempty"`
}

type FieldErrorConstraint struct {
	Max int `json:"max,omitempty"`
	Min int `json:"min,omitempty"`
}

type Validator struct {
	fields []FieldError
	err    error
}

func (v *Validator) Fields() []FieldError {
	if len(v.fields) == 0 {
		return []FieldError{}
	}
	return v.fields
}

func (v *Validator) Append(field, errType string) {
	v.fields = append(v.fields, FieldError{Field: field, Error: errType})
}

func (v *Validator) AppendMax(field string, maxVal int) {
	v.fields = append(v.fields, FieldError{
		Field:      field,
		Error:      FieldErrorMax,
		Constraint: &FieldErrorConstraint{Max: maxVal},
	})
}

func (v *Validator) AppendMin(field string, minVal int) {
	v.fields = append(v.fields, FieldError{
		Field:      field,
		Error:      FieldErrorMin,
		Constraint: &FieldErrorConstraint{Min: minVal},
	})
}

func (v *Validator) SetInternalError(err error) { v.err = err }

func (v *Validator) HasInternalError() bool { return v.err != nil }

func (v *Validator) HasErrors() bool { return len(v.fields) != 0 || v.HasInternalError() }

func (v *Validator) Error() error {
	if v.err != nil {
		return InternalError(v.err)
	} else if len(v.fields) != 0 {
		return ValidationError(v.fields)
	}
	return nil
}

func ValidationError(fieldErrors []FieldError) *zenrpc.Error {
	return &zenrpc.Error{Code: http.StatusBadRequest, Data: fieldErrors, Message: "Validation err"}
}
