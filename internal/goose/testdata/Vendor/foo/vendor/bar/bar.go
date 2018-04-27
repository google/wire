// Package bar is the vendored copy of bar which contains the real provider.
package bar

// ProvideMessage provides a friendly user greeting.
func ProvideMessage() string {
	return "Hello, World!"
}
