package validation

import (
	"fmt"
	"strings"
)

type Requirement struct {
	Name  string
	Value any
}

func Required(name string, value any) Requirement {
	return Requirement{Name: name, Value: value}
}

func MissingRequirements(requirements ...Requirement) []string {
	missing := make([]string, 0)
	for _, requirement := range requirements {
		if IsNil(requirement.Value) {
			missing = append(missing, requirement.Name)
		}
	}
	return missing
}

func RequireAll(message string, requirements ...Requirement) error {
	missing := MissingRequirements(requirements...)
	if len(missing) == 0 {
		return nil
	}
	return fmt.Errorf("%s: %s", message, strings.Join(missing, ", "))
}
