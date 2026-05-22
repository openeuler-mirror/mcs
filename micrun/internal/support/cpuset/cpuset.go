// Package cpuset provides CPU set management functionality.
// This is a local implementation to replace kata-containers dependency.
package cpuset

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
)

const maxCPUSetRangeWidth = 1 << 20

// CPUSet represents a set of CPUs.
type CPUSet struct {
	cpus map[int]struct{}
}

// NewCPUSet creates a new CPUSet with the specified CPUs.
func NewCPUSet(cpus ...int) CPUSet {
	set := newCPUSetWithCapacity(len(cpus))
	for _, cpu := range cpus {
		if cpu < 0 {
			continue
		}
		set.cpus[cpu] = struct{}{}
	}
	return set
}

// Parse parses a CPU set string (e.g., "0,1-3,5") into a CPUSet.
func Parse(s string) (CPUSet, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return newCPUSetWithCapacity(0), nil
	}

	parts := strings.Split(s, ",")
	set := newCPUSetWithCapacity(len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			return set, fmt.Errorf("empty CPU entry in %q", s)
		}

		if err := addCPUEntry(set.cpus, part); err != nil {
			return set, err
		}
	}

	return set, nil
}

func newCPUSetWithCapacity(capacity int) CPUSet {
	return CPUSet{
		cpus: make(map[int]struct{}, capacity),
	}
}

func addCPUEntry(target map[int]struct{}, part string) error {
	if strings.Contains(part, "-") {
		start, end, err := parseCPURange(part)
		if err != nil {
			return err
		}
		for i := start; i <= end; i++ {
			target[i] = struct{}{}
		}
		return nil
	}

	cpu, err := parseNonNegativeCPU(part, fmt.Sprintf("CPU number %s", part))
	if err != nil {
		return err
	}
	target[cpu] = struct{}{}
	return nil
}

func parseCPURange(part string) (int, int, error) {
	rangeParts := strings.Split(part, "-")
	if len(rangeParts) != 2 {
		return 0, 0, fmt.Errorf("invalid CPU range: %s", part)
	}

	start, err := parseNonNegativeCPU(rangeParts[0], fmt.Sprintf("CPU start in range %s", part))
	if err != nil {
		return 0, 0, err
	}
	end, err := parseNonNegativeCPU(rangeParts[1], fmt.Sprintf("CPU end in range %s", part))
	if err != nil {
		return 0, 0, err
	}
	if start > end {
		return 0, 0, fmt.Errorf("invalid CPU range %s: start > end", part)
	}
	if end-start+1 > maxCPUSetRangeWidth {
		return 0, 0, fmt.Errorf("invalid CPU range %s: exceeds maximum width %d", part, maxCPUSetRangeWidth)
	}
	return start, end, nil
}

func parseNonNegativeCPU(raw, label string) (int, error) {
	cpu, err := strconv.Atoi(strings.TrimSpace(raw))
	if err != nil {
		return 0, fmt.Errorf("invalid %s: %w", label, err)
	}
	if cpu < 0 {
		return 0, fmt.Errorf("invalid %s: must be non-negative", label)
	}
	return cpu, nil
}

// Contains checks if the CPU set contains the specified CPU.
func (set CPUSet) Contains(cpu int) bool {
	_, ok := set.cpus[cpu]
	return ok
}

// Size returns the number of CPUs in the set.
func (set CPUSet) Size() int {
	return len(set.cpus)
}

// ToSlice returns the CPUs in the set as a sorted slice.
func (set CPUSet) ToSlice() []int {
	cpus := make([]int, 0, len(set.cpus))
	for cpu := range set.cpus {
		cpus = append(cpus, cpu)
	}
	sort.Ints(cpus)
	return cpus
}

// String returns the string representation of the CPU set.
func (set CPUSet) String() string {
	if len(set.cpus) == 0 {
		return ""
	}

	cpus := set.ToSlice()
	if len(cpus) == 1 {
		return strconv.Itoa(cpus[0])
	}

	parts := make([]string, 0, len(cpus))
	i := 0
	for i < len(cpus) {
		start := cpus[i]
		end := start

		// Find consecutive range
		for i+1 < len(cpus) && cpus[i+1] == cpus[i]+1 {
			i++
			end = cpus[i]
		}

		if start == end {
			parts = append(parts, strconv.Itoa(start))
		} else {
			parts = append(parts, fmt.Sprintf("%d-%d", start, end))
		}
		i++
	}

	return strings.Join(parts, ",")
}

// IsEmpty returns true if the CPU set is empty.
func (set CPUSet) IsEmpty() bool {
	return len(set.cpus) == 0
}

// Equals returns true if the two CPU sets are equal.
func (set CPUSet) Equals(other CPUSet) bool {
	if len(set.cpus) != len(other.cpus) {
		return false
	}

	for cpu := range set.cpus {
		if _, ok := other.cpus[cpu]; !ok {
			return false
		}
	}

	return true
}

// Union returns a new CPU set containing the union of the two sets.
func (set CPUSet) Union(other CPUSet) CPUSet {
	result := newCPUSetWithCapacity(len(set.cpus) + len(other.cpus))
	for cpu := range set.cpus {
		result.cpus[cpu] = struct{}{}
	}
	for cpu := range other.cpus {
		result.cpus[cpu] = struct{}{}
	}
	return result
}

// Intersection returns a new CPU set containing the intersection of the two sets.
func (set CPUSet) Intersection(other CPUSet) CPUSet {
	result := newCPUSetWithCapacity(min(len(set.cpus), len(other.cpus)))
	for cpu := range set.cpus {
		if _, ok := other.cpus[cpu]; ok {
			result.cpus[cpu] = struct{}{}
		}
	}
	return result
}

// Difference returns a new CPU set containing CPUs in set but not in other.
func (set CPUSet) Difference(other CPUSet) CPUSet {
	result := newCPUSetWithCapacity(len(set.cpus))
	for cpu := range set.cpus {
		if _, ok := other.cpus[cpu]; !ok {
			result.cpus[cpu] = struct{}{}
		}
	}
	return result
}
