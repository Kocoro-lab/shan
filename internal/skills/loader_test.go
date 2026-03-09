package skills

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadSkills_Basic(t *testing.T) {
	dir := t.TempDir()
	skillsDir := filepath.Join(dir, "skills")
	os.MkdirAll(skillsDir, 0700)

	yaml := `name: review-pr
description: Review a pull request
type: prompt
trigger: "review PR #(\\d+)"
prompt: |
  Review the PR carefully.
`
	os.WriteFile(filepath.Join(skillsDir, "review-pr.yaml"), []byte(yaml), 0600)

	skills, err := LoadSkills(dir, "review-agent")
	if err != nil {
		t.Fatalf("LoadSkills: %v", err)
	}
	if len(skills) != 1 {
		t.Fatalf("got %d skills, want 1", len(skills))
	}

	s := skills[0]
	if s.Name != "review-pr" {
		t.Errorf("name = %q", s.Name)
	}
	if s.Type != SkillTypePrompt {
		t.Errorf("type = %q", s.Type)
	}
	if s.Source != "review-agent" {
		t.Errorf("source = %q", s.Source)
	}
	if s.Trigger != `review PR #(\d+)` {
		t.Errorf("trigger = %q", s.Trigger)
	}
	if s.Prompt == "" {
		t.Error("prompt should not be empty")
	}
}

func TestLoadSkills_MissingDir(t *testing.T) {
	skills, err := LoadSkills(t.TempDir(), "test")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if skills != nil {
		t.Errorf("expected nil for missing dir, got %v", skills)
	}
}

func TestLoadSkills_MissingName(t *testing.T) {
	dir := t.TempDir()
	skillsDir := filepath.Join(dir, "skills")
	os.MkdirAll(skillsDir, 0700)
	os.WriteFile(filepath.Join(skillsDir, "bad.yaml"), []byte("type: prompt\n"), 0600)

	_, err := LoadSkills(dir, "test")
	if err == nil {
		t.Error("expected error for missing skill name")
	}
}
