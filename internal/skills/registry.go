package skills

// SkillType defines how a skill is executed.
type SkillType string

const (
	// SkillTypePrompt injects a prompt template into the conversation.
	SkillTypePrompt SkillType = "prompt"
)

// Skill is a composable capability loaded from an agent's skills/ directory.
// Currently only prompt-type skills are executed (as TUI slash commands).
type Skill struct {
	Name        string    `yaml:"name"`
	Description string    `yaml:"description"`
	Trigger     string    `yaml:"trigger,omitempty"` // regex pattern for auto-trigger (future use)
	Type        SkillType `yaml:"type"`

	// For SkillTypePrompt
	Prompt string `yaml:"prompt,omitempty"`

	// Metadata
	Source string `yaml:"-"` // which agent defined this
}
