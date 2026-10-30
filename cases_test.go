package main

import (
	"regexp"
	"strings"
	"testing"
)

var leftover = regexp.MustCompile(`\{[A-F]\}|\{회사\}|\{팀장\}`)

// Every bundled case must parse, and casting must fill every placeholder.
func TestBundledCases(t *testing.T) {
	cases, errs := loadCases()
	for _, err := range errs {
		t.Error(err)
	}
	if len(cases) == 0 {
		t.Fatal("no bundled cases")
	}
	for _, d := range cases {
		letters := map[string]bool{}
		for _, s := range d.Suspects {
			letters[s.Letter] = true
			if len(s.Knows) == 0 && s.Letter != d.Culprit {
				t.Errorf("%s: innocent suspect %s has no clues", d.ID, s.Letter)
			}
		}
		names, boss := castNames(len(d.Suspects))
		pairs := []string{"{회사}", "회사", "{팀장}", boss}
		for i, s := range d.Suspects {
			pairs = append(pairs, "{"+s.Letter+"}", names[i])
		}
		r := strings.NewReplacer(pairs...).Replace
		all := []string{d.Title, d.Overview, d.Motive, d.Method, d.Key}
		all = append(all, d.Timeline...)
		for _, s := range d.Suspects {
			all = append(all, s.Claim, s.Truth, s.Secret)
			all = append(all, s.Knows...)
		}
		for _, e := range d.Evidence {
			all = append(all, e.Title, e.Content)
		}
		for _, text := range all {
			if m := leftover.FindString(r(text)); m != "" {
				t.Errorf("%s: placeholder %s is not a suspect letter: %q", d.ID, m, text)
			}
		}
		t.Logf("%s: %d suspects, %d clues, %d evidence, culprit %s", d.ID, len(d.Suspects), countClues(d), len(d.Evidence), d.Culprit)
	}
}

func countClues(d *caseDraft) int {
	n := 0
	for _, s := range d.Suspects {
		n += len(s.Knows)
	}
	return n
}
