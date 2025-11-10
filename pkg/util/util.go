package util

// StringPtr returns a pointer to the string value
func StringPtr(s string) *string {
	return &s
}

// IntPtr returns a pointer to the int value
func IntPtr(i int) *int {
	return &i
}

// Int32Ptr returns a pointer to the int32 value
func Int32Ptr(i int32) *int32 {
	return &i
}

// Int64Ptr returns a pointer to the int64 value
func Int64Ptr(i int64) *int64 {
	return &i
}

// BoolPtr returns a pointer to the bool value
func BoolPtr(b bool) *bool {
	return &b
}

// Float64Ptr returns a pointer to the float64 value
func Float64Ptr(f float64) *float64 {
	return &f
}

// DefaultString returns the string value or a default if empty
func DefaultString(s, defaultValue string) string {
	if s == "" {
		return defaultValue
	}
	return s
}

// DefaultInt returns the int value or a default if zero
func DefaultInt(i, defaultValue int) int {
	if i == 0 {
		return defaultValue
	}
	return i
}

// Contains checks if a string slice contains a value
func Contains(slice []string, value string) bool {
	for _, item := range slice {
		if item == value {
			return true
		}
	}
	return false
}
