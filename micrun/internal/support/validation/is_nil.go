package validation

import (
	"errors"
	"reflect"
)

// IsNil reports whether value is nil, including typed nils wrapped in interfaces.
func IsNil(value any) bool {
	if value == nil {
		return true
	}

	v := reflect.ValueOf(value)
	switch v.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Pointer, reflect.Slice:
		return v.IsNil()
	default:
		return false
	}
}

// RequireNotNil returns a typed error when value is nil (including typed nil interfaces).
func RequireNotNil(value any, message string) error {
	if IsNil(value) {
		return errors.New(message)
	}
	return nil
}
