package metadata

// ValidRunSource retains the observable baseline trigger label. Coalescing keeps
// the original run's source; only cron runs may suppress a quiet report.
func ValidRunSource(source string) bool {
	switch source {
	case "command", "cron", "new", "link", "bind":
		return true
	default:
		return false
	}
}
