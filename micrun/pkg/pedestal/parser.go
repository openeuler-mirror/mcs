package pedestal

import (
	"strconv"
	"strings"
)

// ParseCPUArr translates CPU array to CPU string format
// Example: [1,4,5] -> "1,4-5"
// TODO: use map[] struct{} to store CPU set instead of array
func ParseCPUArr(cpus []int) string {
	if len(cpus) == 0 {
		return ""
	}

	// Sort the CPU array
	sorted := make([]int, len(cpus))
	copy(sorted, cpus)

	// Simple bubble sort for small arrays
	for i := 0; i < len(sorted)-1; i++ {
		for j := 0; j < len(sorted)-i-1; j++ {
			if sorted[j] > sorted[j+1] {
				sorted[j], sorted[j+1] = sorted[j+1], sorted[j]
			}
		}
	}

	// Convert to string format
	var result strings.Builder
	start := sorted[0]
	end := sorted[0]

	for i := 1; i < len(sorted); i++ {
		if sorted[i] == end+1 {
			// Continue the range
			end = sorted[i]
		} else {
			// End the current range and start a new one
			if start == end {
				result.WriteString(strconv.Itoa(start))
			} else {
				result.WriteString(strconv.Itoa(start))
				result.WriteString("-")
				result.WriteString(strconv.Itoa(end))
			}
			result.WriteString(",")
			start = sorted[i]
			end = sorted[i]
		}
	}

	// Add the last range
	if start == end {
		result.WriteString(strconv.Itoa(start))
	} else {
		result.WriteString(strconv.Itoa(start))
		result.WriteString("-")
		result.WriteString(strconv.Itoa(end))
	}

	return result.String()
}
