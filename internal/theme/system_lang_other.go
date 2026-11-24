//go:build !windows && !darwin

package theme

func platformPreferredLanguages() []string {
	return posixPreferredLanguages()
}
