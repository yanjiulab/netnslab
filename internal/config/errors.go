package config

import "fmt"

// FieldError represents a validation error on a specific field.
type FieldError struct {
	Field   string
	Message string
}

func (e *FieldError) Error() string {
	return fmt.Sprintf("invalid field %q: %s", e.Field, e.Message)
}

// MultiError aggregates multiple errors.
type MultiError struct {
	Errors []error
}

func (m *MultiError) Error() string {
	if len(m.Errors) == 0 {
		return "no error"
	}
	if len(m.Errors) == 1 {
		return m.Errors[0].Error()
	}
	return fmt.Sprintf("%d validation errors (first: %v)", len(m.Errors), m.Errors[0])
}

func (m *MultiError) Add(err error) {
	if err == nil {
		return
	}
	m.Errors = append(m.Errors, err)
}

func (m *MultiError) NilOrError() error {
	if len(m.Errors) == 0 {
		return nil
	}
	return m
}

