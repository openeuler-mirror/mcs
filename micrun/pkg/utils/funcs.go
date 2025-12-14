package utils

func FilterSlice[T any](s []T, filter func(T) bool) []T {
	var result []T
	for _, item := range s {
		if filter(item) {
			result = append(result, item)
		}
	}
	return result
}

func MapCheck[T any](m map[string]T, filter func(key string, value T) bool) bool {
	for k, v := range m {
		if filter(k, v) {
			return true
		}
	}
	return false
}
