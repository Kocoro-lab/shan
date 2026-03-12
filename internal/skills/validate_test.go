package skills

import "testing"

func TestValidateSkillName(t *testing.T) {
	tests := []struct {
		name    string
		wantErr bool
	}{
		{"pdf", false},
		{"mcp-builder", false},
		{"a", false},
		{"a1", false},
		{"my-cool-skill", false},
		{"", true},
		{"PDF", true},
		{"my_skill", true},
		{"my--skill", true},
		{"-pdf", true},
		{"pdf-", true},
		{"a b", true},
		{"quit", true},
		{"help", true},
		{"search", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateSkillName(tt.name)
			if (err != nil) != tt.wantErr {
				t.Errorf("ValidateSkillName(%q) error = %v, wantErr %v", tt.name, err, tt.wantErr)
			}
		})
	}
}
