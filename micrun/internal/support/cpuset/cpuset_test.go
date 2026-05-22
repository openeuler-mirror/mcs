package cpuset

import (
	"testing"
)

func TestNewCPUSetEmpty(t *testing.T) {
	s := NewCPUSet()
	if !s.IsEmpty() {
		t.Error("empty CPUSet should be empty")
	}
	if s.Size() != 0 {
		t.Errorf("Size() = %d, want 0", s.Size())
	}
}

func TestNewCPUSetSingle(t *testing.T) {
	s := NewCPUSet(3)
	if s.Size() != 1 {
		t.Errorf("Size() = %d, want 1", s.Size())
	}
	if !s.Contains(3) {
		t.Error("should contain CPU 3")
	}
}

func TestNewCPUSetMultiple(t *testing.T) {
	s := NewCPUSet(0, 2, 5)
	if s.Size() != 3 {
		t.Errorf("Size() = %d, want 3", s.Size())
	}
	for _, cpu := range []int{0, 2, 5} {
		if !s.Contains(cpu) {
			t.Errorf("should contain CPU %d", cpu)
		}
	}
	if s.Contains(1) {
		t.Error("should not contain CPU 1")
	}
}

func TestNewCPUSetSkipsNegativeValues(t *testing.T) {
	s := NewCPUSet(-1, 0, 2)
	if s.Size() != 2 {
		t.Fatalf("Size() = %d, want 2", s.Size())
	}
	if s.Contains(-1) {
		t.Fatal("CPUSet should not contain negative CPU")
	}
}

func TestParseEmpty(t *testing.T) {
	s, err := Parse("")
	if err != nil {
		t.Fatalf("Parse(\"\") error: %v", err)
	}
	if !s.IsEmpty() {
		t.Error("empty string should produce empty CPUSet")
	}
}

func TestParseWhitespaceOnlyIsEmpty(t *testing.T) {
	s, err := Parse(" \t\n ")
	if err != nil {
		t.Fatalf("Parse whitespace error: %v", err)
	}
	if !s.IsEmpty() {
		t.Fatal("whitespace-only input should produce empty CPUSet")
	}
}

func TestParseSingle(t *testing.T) {
	s, err := Parse("3")
	if err != nil {
		t.Fatalf("Parse error: %v", err)
	}
	if s.Size() != 1 || !s.Contains(3) {
		t.Errorf("Parse(\"3\") = %v", s.ToSlice())
	}
}

func TestParseCommaSeparated(t *testing.T) {
	s, err := Parse("0,2,5")
	if err != nil {
		t.Fatalf("Parse error: %v", err)
	}
	got := s.ToSlice()
	want := []int{0, 2, 5}
	if len(got) != len(want) {
		t.Fatalf("ToSlice() = %v, want %v", got, want)
	}
	for i, v := range got {
		if v != want[i] {
			t.Errorf("ToSlice()[%d] = %d, want %d", i, v, want[i])
		}
	}
}

func TestParseRange(t *testing.T) {
	s, err := Parse("0-3")
	if err != nil {
		t.Fatalf("Parse error: %v", err)
	}
	got := s.ToSlice()
	want := []int{0, 1, 2, 3}
	if len(got) != len(want) {
		t.Fatalf("ToSlice() = %v, want %v", got, want)
	}
	for i, v := range got {
		if v != want[i] {
			t.Errorf("ToSlice()[%d] = %d, want %d", i, v, want[i])
		}
	}
}

func TestParseMixed(t *testing.T) {
	s, err := Parse("0,2-4,7")
	if err != nil {
		t.Fatalf("Parse error: %v", err)
	}
	got := s.ToSlice()
	want := []int{0, 2, 3, 4, 7}
	if len(got) != len(want) {
		t.Fatalf("ToSlice() = %v, want %v", got, want)
	}
	for i, v := range got {
		if v != want[i] {
			t.Errorf("ToSlice()[%d] = %d, want %d", i, v, want[i])
		}
	}
}

func TestParseInvalidRange(t *testing.T) {
	_, err := Parse("3-1")
	if err == nil {
		t.Fatal("expected error for start > end")
	}
}

func TestParseNonNumeric(t *testing.T) {
	_, err := Parse("abc")
	if err == nil {
		t.Fatal("expected error for non-numeric")
	}
}

func TestParseRejectsEmptyEntry(t *testing.T) {
	for _, input := range []string{"0,,2", ",1", "1,"} {
		t.Run(input, func(t *testing.T) {
			if _, err := Parse(input); err == nil {
				t.Fatalf("Parse(%q) expected error, got nil", input)
			}
		})
	}
}

func TestParseRejectsNegativeCPU(t *testing.T) {
	for _, input := range []string{"-1", "-1-2", "1--2"} {
		t.Run(input, func(t *testing.T) {
			if _, err := Parse(input); err == nil {
				t.Fatalf("Parse(%q) expected error, got nil", input)
			}
		})
	}
}

func TestParseRejectsHugeRange(t *testing.T) {
	if _, err := Parse("0-1048576"); err == nil {
		t.Fatal("expected error for huge range")
	}
}

func TestStringRoundTrip(t *testing.T) {
	cases := []string{"0", "0-3", "0,2,5", "0,2-4,7"}
	for _, tc := range cases {
		s, err := Parse(tc)
		if err != nil {
			t.Fatalf("Parse(%q) error: %v", tc, err)
		}
		normalized := s.String()
		s2, err := Parse(normalized)
		if err != nil {
			t.Fatalf("Parse(normalized %q) error: %v", normalized, err)
		}
		if !s.Equals(s2) {
			t.Errorf("round-trip failed: %q → %q → not equal", tc, normalized)
		}
	}
}

func TestStringEmpty(t *testing.T) {
	s := NewCPUSet()
	if s.String() != "" {
		t.Errorf("String() = %q, want empty", s.String())
	}
}

func TestEquals(t *testing.T) {
	a := NewCPUSet(0, 1, 2)
	b := NewCPUSet(0, 1, 2)
	c := NewCPUSet(0, 1)

	if !a.Equals(b) {
		t.Error("identical sets should be equal")
	}
	if a.Equals(c) {
		t.Error("different sized sets should not be equal")
	}
}

func TestUnion(t *testing.T) {
	a := NewCPUSet(0, 1)
	b := NewCPUSet(1, 2)
	u := a.Union(b)
	want := NewCPUSet(0, 1, 2)
	if !u.Equals(want) {
		t.Errorf("Union = %v, want %v", u.ToSlice(), want.ToSlice())
	}
}

func TestIntersection(t *testing.T) {
	a := NewCPUSet(0, 1, 2)
	b := NewCPUSet(1, 2, 3)
	i := a.Intersection(b)
	want := NewCPUSet(1, 2)
	if !i.Equals(want) {
		t.Errorf("Intersection = %v, want %v", i.ToSlice(), want.ToSlice())
	}
}

func TestDifference(t *testing.T) {
	a := NewCPUSet(0, 1, 2)
	b := NewCPUSet(1, 2, 3)
	d := a.Difference(b)
	want := NewCPUSet(0)
	if !d.Equals(want) {
		t.Errorf("Difference = %v, want %v", d.ToSlice(), want.ToSlice())
	}
}

func TestToSliceSorted(t *testing.T) {
	s := NewCPUSet(5, 1, 3)
	got := s.ToSlice()
	want := []int{1, 3, 5}
	if len(got) != len(want) {
		t.Fatalf("ToSlice() = %v, want %v", got, want)
	}
	for i, v := range got {
		if v != want[i] {
			t.Errorf("ToSlice()[%d] = %d, want %d", i, v, want[i])
		}
	}
}
