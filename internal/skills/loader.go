package skills

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"

	"gopkg.in/yaml.v3"
)

// LoadSkills loads skill definitions from an agent's skills/ directory.
// Each .yaml file in the directory defines one skill.
// Returns nil if the directory doesn't exist.
func LoadSkills(agentDir, agentName string) ([]*Skill, error) {
	skillsDir := filepath.Join(agentDir, "skills")
	pattern := filepath.Join(skillsDir, "*.yaml")
	matches, err := filepath.Glob(pattern)
	if err != nil || len(matches) == 0 {
		return nil, nil
	}
	sort.Strings(matches)

	var skills []*Skill
	for _, path := range matches {
		s, err := loadSkillFile(path, agentName)
		if err != nil {
			return nil, fmt.Errorf("loading %s: %w", filepath.Base(path), err)
		}
		skills = append(skills, s)
	}
	return skills, nil
}

func loadSkillFile(path, agentName string) (*Skill, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var s Skill
	if err := yaml.Unmarshal(data, &s); err != nil {
		return nil, fmt.Errorf("parse error: %w", err)
	}
	if s.Name == "" {
		return nil, fmt.Errorf("skill name is required")
	}
	s.Source = agentName
	return &s, nil
}
