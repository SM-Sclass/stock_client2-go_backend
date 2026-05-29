package utils

// Returns nil if the string is empty, otherwise returns a pointer to the string
func ToNullString(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

// Returns nil if the float is 0, otherwise returns a pointer to the float
func ToNullFloat(f float64) *float64 {
	if f == 0 {
		return nil
	}
	return &f
}