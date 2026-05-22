package validation

import "testing"

func TestIsNil(t *testing.T) {
	t.Run("nil interface", func(t *testing.T) {
		var value any
		if !IsNil(value) {
			t.Fatal("expected nil interface to be detected as nil")
		}
	})

	t.Run("typed nil pointer", func(t *testing.T) {
		var p *int
		var value any = p
		if !IsNil(value) {
			t.Fatal("expected typed nil pointer to be detected as nil")
		}
	})

	t.Run("typed nil map", func(t *testing.T) {
		var m map[string]string
		var value any = m
		if !IsNil(value) {
			t.Fatal("expected typed nil map to be detected as nil")
		}
	})

	t.Run("typed nil func", func(t *testing.T) {
		var fn func()
		var value any = fn
		if !IsNil(value) {
			t.Fatal("expected typed nil func to be detected as nil")
		}
	})

	t.Run("non nil value", func(t *testing.T) {
		if IsNil(42) {
			t.Fatal("expected non nil value to be detected as non nil")
		}
	})
}

func TestRequireNotNil(t *testing.T) {
	t.Run("nil fails", func(t *testing.T) {
		if err := RequireNotNil(nil, "value is required"); err == nil {
			t.Fatal("expected error when value is nil")
		}
	})

	t.Run("typed nil fails", func(t *testing.T) {
		var fn func()
		if err := RequireNotNil(fn, "value is required"); err == nil {
			t.Fatal("expected error for typed nil")
		}
	})

	t.Run("non nil passes", func(t *testing.T) {
		if err := RequireNotNil("ok", "value is required"); err != nil {
			t.Fatalf("expected no error, got: %v", err)
		}
	})
}
