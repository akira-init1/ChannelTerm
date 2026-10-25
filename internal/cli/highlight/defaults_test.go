package highlight

import (
	"encoding/json"
	"os"
	"reflect"
	"regexp"
	"testing"
)

func TestSharedDefaultHighlightExamples(t *testing.T) {
	data, err := os.ReadFile("testdata/defaults-cases.json")
	if err != nil {
		t.Fatal(err)
	}
	var examples struct {
		Rules []struct {
			Rule    int      `json:"rule"`
			Input   string   `json:"input"`
			Matches []string `json:"matches"`
		} `json:"rules"`
		Prompts []struct {
			Pattern  string   `json:"pattern"`
			Input    string   `json:"input"`
			Captures []string `json:"captures"`
		} `json:"prompts"`
	}
	if err := json.Unmarshal(data, &examples); err != nil {
		t.Fatal(err)
	}
	seen := make(map[int]bool)
	for _, example := range examples.Rules {
		rule := builtinRules[example.Rule]
		seen[example.Rule] = true
		var actual []string
		for _, match := range rule.pattern.FindAllStringSubmatch(example.Input, -1) {
			actual = append(actual, match[rule.group])
		}
		if !reflect.DeepEqual(actual, example.Matches) {
			t.Errorf("rule %d (%s): got %q, want %q", example.Rule, rule.kind, actual, example.Matches)
		}
		if rule.pattern.MatchString("plain") {
			t.Errorf("rule %d matches unrelated text", example.Rule)
		}
	}
	if len(seen) != len(builtinRules) {
		t.Fatal("shared corpus must cover every rule")
	}
	for _, example := range examples.Prompts {
		pattern := regexp.MustCompile(defaults.Prompts[example.Pattern])
		actual := pattern.FindStringSubmatch(example.Input)
		if actual == nil || !reflect.DeepEqual(actual[1:], example.Captures) {
			t.Errorf("prompt %s: got %q, want captures %q", example.Pattern, actual, example.Captures)
		}
		if pattern.MatchString("log: " + example.Input) {
			t.Errorf("prompt %s accepts a non-prefix", example.Pattern)
		}
	}
}

func TestSharedDefaultHighlightRejectsInvalidRules(t *testing.T) {
	for _, definition := range []ruleDefinition{
		{Pattern: "x", Flags: "g", Kind: tokenError},
		{Pattern: "x", Group: 1, Kind: tokenError},
		{Pattern: "x", Kind: "missing"},
		{Pattern: "[", Kind: tokenError},
	} {
		t.Run(definition.Pattern+definition.Flags+string(definition.Kind), func(t *testing.T) {
			defer func() {
				if recover() == nil {
					t.Fatal("invalid embedded rule accepted")
				}
			}()
			compileRules(highlightDefinitions{Palette: defaults.Palette, Rules: []ruleDefinition{definition}})
		})
	}
}
