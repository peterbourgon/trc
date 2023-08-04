package trcutil

// FlattenErrors converts a slice of errors to a slice of strings.
func FlattenErrors(errs ...error) []string {
	if len(errs) <= 0 {
		return nil
	}
	strs := make([]string, len(errs))
	for i := range errs {
		strs[i] = errs[i].Error()
	}
	return strs
}
