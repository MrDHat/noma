package config_test

import (
	"bytes"
	"reflect"
	"testing"

	"github.com/MrDHat/noma/internal/config"
)

func TestOverridesResolveDeclaredConfiguration(t *testing.T) {
	tests := []struct {
		name string
		opts config.Options
		want config.Project
	}{
		{"defaults", config.Options{}, config.Project{Agent: config.Claude, Environment: config.Environment{Image: "node:22", Setup: []string{"npm install"}}, Compute: config.Compute{Provider: "aws", Region: "eu-west-1", InstanceType: "t3.large"}, Repos: []string{"acme/default"}}},
		{"profile and overrides", config.Options{Profile: "fullstack", Overrides: []string{"agent=codex", "agent=opencode", `environment.setup=["go test ./..."]`, "profiles.fullstack.repos[0]=acme/next", "compute.instanceType=t3.xlarge"}}, config.Project{Agent: config.OpenCode, Environment: config.Environment{Image: "node:22", Setup: []string{"go test ./..."}}, Compute: config.Compute{Provider: "aws", Region: "eu-west-1", InstanceType: "t3.xlarge"}, Repos: []string{"acme/next", "acme/api"}}},
		{"quoted key", config.Options{Profile: "web.apps", Overrides: []string{`profiles["web.apps"].repos[0]=acme/quoted`}}, config.Project{Agent: config.Claude, Environment: config.Environment{Image: "node:22", Setup: []string{"npm install"}}, Compute: config.Compute{Provider: "aws", Region: "eu-west-1", InstanceType: "t3.large"}, Repos: []string{"acme/quoted"}}},
		{"equals in value", config.Options{Overrides: []string{"environment.image=registry/image:tag=value"}}, config.Project{Agent: config.Claude, Environment: config.Environment{Image: "registry/image:tag=value", Setup: []string{"npm install"}}, Compute: config.Compute{Provider: "aws", Region: "eu-west-1", InstanceType: "t3.large"}, Repos: []string{"acme/default"}}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			data := []byte(projectYAML)
			original := bytes.Clone(data)
			got, err := config.Parse(data, tt.opts)
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("got %#v, want %#v", got, tt.want)
			}
			if !bytes.Equal(data, original) {
				t.Fatal("Parse changed its input")
			}
		})
	}
}

func TestOverridesSupplyRequiredFields(t *testing.T) {
	for _, agent := range []config.Agent{config.Claude, config.Codex, config.OpenCode} {
		got, err := config.Parse([]byte("{}"), config.Options{Overrides: []string{"agent=" + string(agent), "environment.image=golang:1.26", `compute={"provider":"local"}`}})
		if err != nil {
			t.Fatal(err)
		}
		if got.Agent != agent || got.Environment.Image != "golang:1.26" || got.Compute.Provider != "local" || len(got.Repos) != 0 {
			t.Fatalf("unexpected project %#v", got)
		}
	}
	got, err := config.Parse([]byte("agent: codex\nenvironment: {image: '123'}\ncompute: {provider: custom}\n"), config.Options{})
	if err != nil || got.Environment.Image != "123" {
		t.Fatalf("quoted string rejected: %#v, %v", got, err)
	}
}

func TestParseRejectsInvalidOverrides(t *testing.T) {
	for _, override := range []string{
		"agent", "=codex", ".agent=codex", "agent.=codex", "agent..name=codex", "agent[=codex", "agent[]=codex", "agent[-1]=codex", "agent[0]extra=codex", "environment.setup[99]=x", "environment.setup[1]=x", "environment.setup[0].x=y", "agent.name=x", "environment[0]=x", `profiles["oops].repos=[]`, `profiles["fullstack"]repos=[]`, "environment.image=123", "environment.image=true", "environment.image=null", "environment.setup=[null]", "environment.setup=[1]", "environment.image=[", "environment.image={", `environment.image="`, "unknown=value", "Environment.image=value", "environment.setup[999999999999999999999999]=x", "environment.setup[0][0]=x",
	} {
		t.Run(override, func(t *testing.T) {
			if _, err := config.Parse([]byte(projectYAML), config.Options{Overrides: []string{override}}); err == nil {
				t.Fatal("accepted invalid override")
			}
		})
	}
	if _, err := config.Parse([]byte(projectYAML), config.Options{Profile: "missing"}); err == nil {
		t.Fatal("accepted unknown profile")
	}
	if _, err := config.Parse([]byte(projectYAML+"\nagent: codex"), config.Options{Overrides: []string{"agent=opencode"}}); err == nil {
		t.Fatal("override hid duplicate YAML key")
	}
}

func TestOverridesRejectDuplicateObjectKeys(t *testing.T) {
	for _, override := range []string{
		`environment={"image":"alpine","image":"ubuntu"}`,
		`environment={"image":"alpine","\u0069mage":"ubuntu"}`,
		`profiles={"😀":{"repos":["acme/a"]},"\ud83d\ude00":{"repos":["acme/b"]}}`,
		`profiles={"fullstack":{"repos":["acme/a"],"repos":["acme/b"]}}`,
		`profiles={"fullstack":{"repos":["acme/a"]},"fullstack":{"repos":["acme/b"]}}`,
	} {
		t.Run(override, func(t *testing.T) {
			if _, err := config.Parse([]byte(projectYAML), config.Options{Overrides: []string{override}}); err == nil {
				t.Fatal("accepted duplicate JSON object key")
			}
		})
	}
}

func TestOverridesAddressQuotedKeysContainingEquals(t *testing.T) {
	for _, tt := range []struct{ profile, selector string }{
		{"team=blue", `profiles["team=blue"].repos[0]`},
		{`team"=blue`, `profiles["team\"=blue"].repos[0]`},
		{`team\=blue`, `profiles["team\\=blue"].repos[0]`},
	} {
		t.Run(tt.profile, func(t *testing.T) {
			data := []byte(projectYAML + "\n  '" + tt.profile + "':\n    repos: [acme/old]\n")
			got, err := config.Parse(data, config.Options{Profile: tt.profile, Overrides: []string{tt.selector + "=acme/next=repo"}})
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(got.Repos, []string{"acme/next=repo"}) {
				t.Fatalf("unexpected repositories %#v", got.Repos)
			}
		})
	}
}

func TestOverridesPreserveJSONSurrogateEscapes(t *testing.T) {
	for _, tt := range []struct {
		name     string
		override string
		image    string
		setup    []string
	}{
		{"scalar", `environment.image="\ud83d\ude00"`, "😀", []string{"npm install"}},
		{"array", `environment.setup=["echo \ud83d\ude00"]`, "node:22", []string{"echo 😀"}},
		{"object", `environment={"image":"emoji-\ud83d\ude00","setup":["echo \ud83d\ude00"]}`, "emoji-😀", []string{"echo 😀"}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			data := []byte(projectYAML)
			original := bytes.Clone(data)
			got, err := config.Parse(data, config.Options{Overrides: []string{tt.override}})
			if err != nil {
				t.Fatal(err)
			}
			if got.Environment.Image != tt.image || !reflect.DeepEqual(got.Environment.Setup, tt.setup) {
				t.Fatalf("unexpected environment %#v", got.Environment)
			}
			if !bytes.Equal(data, original) {
				t.Fatal("Parse changed its input")
			}
		})
	}
}
