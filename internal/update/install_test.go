package update

import "testing"

func TestIsHomebrewPath(t *testing.T) {
	tests := []struct {
		path string
		want bool
	}{
		{"/opt/homebrew/Cellar/shan/0.1.2/bin/shan", true},
		{"/usr/local/Cellar/shan/0.1.2/bin/shan", true},
		{"/opt/homebrew/bin/shan", true},
		{"/usr/local/bin/shan", false},
		{"/home/user/go/bin/shan", false},
		{"", false},
	}
	for _, tt := range tests {
		if got := IsHomebrewPath(tt.path); got != tt.want {
			t.Errorf("IsHomebrewPath(%q) = %v, want %v", tt.path, got, tt.want)
		}
	}
}
