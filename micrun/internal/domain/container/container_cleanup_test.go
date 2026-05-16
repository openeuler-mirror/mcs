package container

import (
	"fmt"
	"testing"

	er "micrun/internal/support/errors"
)

func TestTolerableAcceptsWrappedContainerNotFound(t *testing.T) {
	if !tolerable(fmt.Errorf("wrapped: %w", er.ContainerNotFound), false) {
		t.Fatal("wrapped ContainerNotFound should be tolerable")
	}
}

func TestTolerableRejectsNonNotFoundErrorsWithoutForce(t *testing.T) {
	if tolerable(fmt.Errorf("other error"), false) {
		t.Fatal("non-not-found error should not be tolerable without force")
	}
}
