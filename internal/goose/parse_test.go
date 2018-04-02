package goose

import (
	"testing"
)

func TestDirectiveArgs(t *testing.T) {
	tests := []struct {
		line string
		args []string
	}{
		{"", []string{}},
		{"  \t  ", []string{}},
		{"foo", []string{"foo"}},
		{"foo bar", []string{"foo", "bar"}},
		{"  foo \t bar  ", []string{"foo", "bar"}},
		{"foo \"bar \t baz\" fido", []string{"foo", "\"bar \t baz\"", "fido"}},
		{"foo \"bar \t baz\".quux fido", []string{"foo", "\"bar \t baz\".quux", "fido"}},
	}
	eq := func(a, b []string) bool {
		if len(a) != len(b) {
			return false
		}
		for i := range a {
			if a[i] != b[i] {
				return false
			}
		}
		return true
	}
	for _, test := range tests {
		got := (directive{line: test.line}).args()
		if !eq(got, test.args) {
			t.Errorf("directive{line: %q}.args() = %q; want %q", test.line, got, test.args)
		}
	}
}
