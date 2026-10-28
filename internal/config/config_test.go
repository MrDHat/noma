package config_test

import (
	"bytes"
	"reflect"
	"strings"
	"testing"

	"github.com/MrDHat/noma/internal/config"
)

const projectYAML = `agent: claude
environment:
  image: node:22
  setup: [npm install]
compute:
  provider: aws
  region: eu-west-1
  instanceType: t3.large
repos: [acme/default]
profiles:
  fullstack:
    repos: [acme/web, acme/api]
  web.apps:
    repos: [acme/apps]
`

func TestParseRejectsInvalidDocuments(t *testing.T) {
	tests := []struct{ name, data string }{
		{"empty", ""},
		{"comment only", "# comment\n"},
		{"sequence root", "[a, b]"},
		{"root null", "null"},
		{"syntax", "agent: ["},
		{"multiple documents", projectYAML + "\n---\n{}"},
		{"empty second document", projectYAML + "\n---\n"},
		{"duplicate root key", projectYAML + "\nagent: codex"},
		{"duplicate nested key", strings.Replace(projectYAML, "  image: node:22", "  image: node:22\n  image: node:24", 1)},
		{"unknown root key", projectYAML + "\nextra: true"},
		{"wrong root key case", strings.Replace(projectYAML, "agent:", "Agent:", 1)},
		{"wrong nested key case", strings.Replace(projectYAML, "instanceType:", "instancetype:", 1)},
		{"unknown environment key", strings.Replace(projectYAML, "  image: node:22", "  image: node:22\n  extra: true", 1)},
		{"profile settings", strings.Replace(projectYAML, "  fullstack:", "  fullstack:\n    agent: codex", 1)},
		{"invalid unselected profile", projectYAML + "\n  bad:\n    repos: []"},
		{"blank profile name", projectYAML + "\n  ' ':\n    repos: [acme/a]"},
		{"missing agent", strings.Replace(projectYAML, "agent: claude\n", "", 1)},
		{"unknown agent", strings.Replace(projectYAML, "agent: claude", "agent: custom", 1)},
		{"missing image", strings.Replace(projectYAML, "  image: node:22\n", "", 1)},
		{"blank image", strings.Replace(projectYAML, "image: node:22", "image: ' '", 1)},
		{"blank provider", strings.Replace(projectYAML, "provider: aws", "provider: ' '", 1)},
		{"blank optional string", strings.Replace(projectYAML, "region: eu-west-1", "region: ' '", 1)},
		{"blank setup command", strings.Replace(projectYAML, "[npm install]", "[' ']", 1)},
		{"blank repo", strings.Replace(projectYAML, "[acme/default]", "[' ']", 1)},
		{"numeric image", strings.Replace(projectYAML, "image: node:22", "image: 123", 1)},
		{"bool image", strings.Replace(projectYAML, "image: node:22", "image: true", 1)},
		{"null optional field", strings.Replace(projectYAML, "region: eu-west-1", "region: null", 1)},
		{"null nested item", strings.Replace(projectYAML, "[npm install]", "[null]", 1)},
		{"nonstring key", projectYAML + "\n123: value"},
		{"anchor alias", strings.Replace(projectYAML, "[acme/default]", "&repos [acme/default]", 1) + "\nother: *repos"},
		{"merge key", projectYAML + "\n<<: {agent: codex}"},
		{"custom tag", strings.Replace(projectYAML, "image: node:22", "image: !custom value", 1)},
		{"nonfinite number", strings.Replace(projectYAML, "image: node:22", "image: .inf", 1)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			data := []byte(tt.data)
			original := bytes.Clone(data)
			got, err := config.Parse(data, config.Options{})
			if err == nil {
				t.Fatalf("accepted invalid input: %#v", got)
			}
			if !reflect.DeepEqual(got, config.Project{}) {
				t.Fatalf("returned partial result %#v", got)
			}
			if !bytes.Equal(data, original) {
				t.Fatal("Parse changed rejected input")
			}
		})
	}
}

func TestParseSelectsProfile(t *testing.T) {
	for _, tt := range []struct {
		profile string
		repos   []string
	}{
		{"", []string{"acme/default"}},
		{"fullstack", []string{"acme/web", "acme/api"}},
		{"web.apps", []string{"acme/apps"}},
	} {
		got, err := config.Parse([]byte(projectYAML), config.Options{Profile: tt.profile})
		if err != nil {
			t.Fatal(err)
		}
		if got.Agent != config.Claude || got.Environment.Image != "node:22" || got.Compute.Provider != "aws" || !reflect.DeepEqual(got.Repos, tt.repos) {
			t.Fatalf("unexpected project %#v", got)
		}
	}
}

func TestParseAcceptsSupportedAgentsWithoutDeclaredRepositories(t *testing.T) {
	for _, agent := range []config.Agent{config.Claude, config.Codex, config.OpenCode} {
		data := "agent: " + string(agent) + "\nenvironment: {image: golang:1.26}\ncompute: {provider: local}\n"
		got, err := config.Parse([]byte(data), config.Options{})
		if err != nil {
			t.Fatal(err)
		}
		if got.Agent != agent || len(got.Repos) != 0 {
			t.Fatalf("unexpected project %#v", got)
		}
	}
}

func TestParseRejectsUnknownProfile(t *testing.T) {
	if _, err := config.Parse([]byte(projectYAML), config.Options{Profile: "missing"}); err == nil {
		t.Fatal("accepted unknown profile")
	}
}

func TestParsePreservesAcceptedInput(t *testing.T) {
	data := []byte(projectYAML)
	original := bytes.Clone(data)
	if _, err := config.Parse(data, config.Options{}); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(data, original) {
		t.Fatal("Parse changed its input")
	}
}
