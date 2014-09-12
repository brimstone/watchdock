package docker

import (
	"testing"
)

# http://www.youtube.com/watch?v=XCsL89YtqCs

func TestHello(t *testing.T) {
	var tests = []struct {
		s, want string
	}{
		{"", "hi there!"},
	}
	for _, c := range tests {
		got := Hello()
		if got != c.want {
			t.Errorf("Hello() == %q, want %q", got, c.want)
		}
	}
}
