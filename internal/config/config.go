package config

import (
	"bytes"
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"go.yaml.in/yaml/v3"
)

type Agent string

const (
	Claude   Agent = "claude"
	Codex    Agent = "codex"
	OpenCode Agent = "opencode"
)

type Environment struct {
	Image string   `json:"image" yaml:"image"`
	Setup []string `json:"setup,omitempty" yaml:"setup,omitempty"`
}

type Compute struct {
	Provider     string `json:"provider" yaml:"provider"`
	Region       string `json:"region,omitempty" yaml:"region,omitempty"`
	InstanceType string `json:"instanceType,omitempty" yaml:"instanceType,omitempty"`
}

type Project struct {
	Agent       Agent       `json:"agent" yaml:"agent"`
	Environment Environment `json:"environment" yaml:"environment"`
	Compute     Compute     `json:"compute" yaml:"compute"`
	Repos       []string    `json:"repos,omitempty" yaml:"repos,omitempty"`
}

type Options struct {
	Profile string
}

type source struct {
	Project  `yaml:",inline"`
	Profiles map[string]profile `json:"profiles,omitempty" yaml:"profiles,omitempty"`
}

type profile struct {
	Repos []string `json:"repos" yaml:"repos"`
}

// Parse returns validated declared configuration without reading Git or changing data.
func Parse(data []byte, options Options) (Project, error) {
	document, err := parseDocument(data)
	if err != nil {
		return Project{}, err
	}
	encoded, err := json.Marshal(document)
	if err != nil {
		return Project{}, fmt.Errorf("encode configuration: %w", err)
	}
	var input source
	decoder := yaml.NewDecoder(bytes.NewReader(encoded))
	decoder.KnownFields(true)
	if err := decoder.Decode(&input); err != nil {
		return Project{}, fmt.Errorf("configuration schema: %w", err)
	}
	if err := json.Unmarshal(encoded, &input); err != nil {
		return Project{}, fmt.Errorf("configuration types: %w", err)
	}
	if err := input.validate(); err != nil {
		return Project{}, err
	}
	if options.Profile != "" {
		selected, ok := input.Profiles[options.Profile]
		if !ok {
			return Project{}, fmt.Errorf("profile %q is not defined", options.Profile)
		}
		input.Repos = selected.Repos
	}
	return input.Project, nil
}

func (s source) validate() error {
	switch s.Agent {
	case Claude, Codex, OpenCode:
	default:
		return fmt.Errorf("agent must be claude, codex, or opencode")
	}
	for _, field := range []struct{ path, value string }{
		{"environment.image", s.Environment.Image},
		{"compute.provider", s.Compute.Provider},
	} {
		if strings.TrimSpace(field.value) == "" {
			return fmt.Errorf("%s must be a nonblank string", field.path)
		}
	}
	for _, field := range []struct{ path, value string }{
		{"compute.region", s.Compute.Region},
		{"compute.instanceType", s.Compute.InstanceType},
	} {
		if field.value != "" && strings.TrimSpace(field.value) == "" {
			return fmt.Errorf("%s must not contain only whitespace", field.path)
		}
	}
	if err := validateStrings(s.Environment.Setup, "environment.setup"); err != nil {
		return err
	}
	if err := validateStrings(s.Repos, "repos"); err != nil {
		return err
	}
	names := make([]string, 0, len(s.Profiles))
	for name := range s.Profiles {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		if strings.TrimSpace(name) == "" {
			return fmt.Errorf("profile names must not be blank")
		}
		path := fmt.Sprintf("profiles[%q].repos", name)
		if len(s.Profiles[name].Repos) == 0 {
			return fmt.Errorf("%s must contain at least one repository", path)
		}
		if err := validateStrings(s.Profiles[name].Repos, path); err != nil {
			return err
		}
	}
	return nil
}

func validateStrings(values []string, path string) error {
	for i, value := range values {
		if strings.TrimSpace(value) == "" {
			return fmt.Errorf("%s[%d] must be a nonblank string", path, i)
		}
	}
	return nil
}
