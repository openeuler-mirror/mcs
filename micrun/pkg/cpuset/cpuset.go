// Package cpuset provides CPU set management functionality.
// This is a local implementation to replace kata-containers dependency.
package cpuset

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
)

// CPUSet represents a set of CPUs.
type CPUSet struct {
	cpus map[int]bool
}

// NewCPUSet creates a new CPUSet with the specified CPUs.
func NewCPUSet(cpus ...int) CPUSet {
	set := CPUSet{
		cpus: make(map[int]bool),
	}
	for _, cpu := range cpus {
		set.cpus[cpu] = true
	}
	return set
}

// Parse parses a CPU set string (e.g., "0,1-3,5") into a CPUSet.
func Parse(s string) (CPUSet, error) {
	set := CPUSet{
		cpus: make(map[int]bool),
	}

	if s == "" {
		return set, nil
	}

	parts := strings.Split(s, ",")
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}

		if strings.Contains(part, "-") {
			// Handle range like "0-3"
			rangeParts := strings.Split(part, "-")
			if len(rangeParts) != 2 {
				return set, fmt.Errorf("invalid CPU range: %s", part)
			}

			start, err := strconv.Atoi(strings.TrimSpace(rangeParts[0]))
			if err != nil {
				return set, fmt.Errorf("invalid CPU start in range %s: %v", part, err)
			}

			end, err := strconv.Atoi(strings.TrimSpace(rangeParts[1]))
			if err != nil {
				return set, fmt.Errorf("invalid CPU end in range %s: %v", part, err)
			}

			if start > end {
				return set, fmt.Errorf("invalid CPU range %s: start > end", part)
			}

			for i := start; i <= end; i++ {
				set.cpus[i] = true
			}
		} else {
			// Handle single CPU
			cpu, err := strconv.Atoi(part)
			if err != nil {
				return set, fmt.Errorf("invalid CPU number %s: %v", part, err)
			}
			set.cpus[cpu] = true
		}
	}

	return set, nil
}

// Contains checks if the CPU set contains the specified CPU.
func (set CPUSet) Contains(cpu int) bool {
	return set.cpus[cpu]
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

	var parts []string
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
		if !other.cpus[cpu] {
			return false
		}
	}

	return true
}

// Union returns a new CPU set containing the union of the two sets.
func (set CPUSet) Union(other CPUSet) CPUSet {
	result := NewCPUSet()
	for cpu := range set.cpus {
		result.cpus[cpu] = true
	}
	for cpu := range other.cpus {
		result.cpus[cpu] = true
	}
	return result
}

// Intersection returns a new CPU set containing the intersection of the two sets.
func (set CPUSet) Intersection(other CPUSet) CPUSet {
	result := NewCPUSet()
	for cpu := range set.cpus {
		if other.cpus[cpu] {
			result.cpus[cpu] = true
		}
	}
	return result
}

// Difference returns a new CPU set containing CPUs in set but not in other.
func (set CPUSet) Difference(other CPUSet) CPUSet {
	result := NewCPUSet()
	for cpu := range set.cpus {
		if !other.cpus[cpu] {
			result.cpus[cpu] = true
		}
	}
	return result
}