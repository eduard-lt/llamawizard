package main

import (
	"reflect"
	"testing"
)

// aliasPair is one (alias, full) pair the CLI accepts. prefix is the
// parent path (empty for top-level commands); alias and full are the
// command tokens that must resolve identically.
type aliasPair struct {
	prefix []string
	alias  string
	full   string
}

// aliasPairs lists every alias the CLI accepts, mirroring the Aliases
// fields in tree.go.
var aliasPairs = []aliasPair{
	{nil, "st", "status"},
	{nil, "dr", "doctor"},
	{nil, "lg", "logs"},
	{nil, "re", "restart"},
	{nil, "wl", "warlock"},
	{nil, "m", "models"},
	{nil, "cfg", "config"},
	{nil, "up", "update"},
	{nil, "v", "version"},
	{nil, "h", "help"},
	{[]string{"models"}, "ls", "list"},
	{[]string{"models"}, "a", "add"},
	{[]string{"models"}, "sh", "show"},
	{[]string{"models"}, "rm", "remove"},
	{[]string{"config"}, "sh", "show"},
	{[]string{"config"}, "p", "path"},
}

// TestAliasResolvesLikeFullForm checks that every alias resolves to the
// identical (path, rest) as its full form, with wizard=false and no error,
// across a range of argument sets.
func TestAliasResolvesLikeFullForm(t *testing.T) {
	// Standard passthrough sets for leaf commands.
	leafSets := [][]string{
		{},
		{"--link", "u", "n"},
		{"-f", "-n", "10"},
		{"x", "--yes"},
	}
	// Nouns take subcommands, so the flag-like sets above are not
	// passthroughs for them; each noun also gets real subcommand
	// passthroughs.
	nounSets := map[string][][]string{
		"models": {
			{"list"},
			{"add", "--link", "u", "n"},
			{"show", "x"},
			{"remove", "x"},
		},
		"config": {
			{"show"},
			{"path"},
		},
	}

	for _, p := range aliasPairs {
		sets := leafSets
		if extra, ok := nounSets[p.full]; ok {
			sets = append(append([][]string{}, leafSets...), extra...)
		}
		for _, args := range sets {
			aliasArgs := append(append(append([]string{}, p.prefix...), p.alias), args...)
			fullArgs := append(append(append([]string{}, p.prefix...), p.full), args...)

			aPath, aRest, aWizard, aErr := resolve(aliasArgs)
			fPath, fRest, fWizard, fErr := resolve(fullArgs)

			if (aErr == nil) != (fErr == nil) {
				t.Errorf("resolve(%v) err = %v, resolve(%v) err = %v: mismatch",
					aliasArgs, aErr, fullArgs, fErr)
				continue
			}
			if aErr != nil {
				// Both forms must fail identically (e.g. a noun given a
				// flag-like first argument).
				if aErr.Error() != fErr.Error() {
					t.Errorf("resolve(%v) error = %q, resolve(%v) error = %q",
						aliasArgs, aErr, fullArgs, fErr)
				}
				continue
			}
			if aWizard || fWizard {
				t.Errorf("resolve(%v): wizard = true, want false", aliasArgs)
			}
			if aPath != fPath {
				t.Errorf("resolve(%v) path = %q, resolve(%v) path = %q",
					aliasArgs, aPath, fullArgs, fPath)
			}
			if !sameStrings(aRest, fRest) {
				t.Errorf("resolve(%v) rest = %v, resolve(%v) rest = %v",
					aliasArgs, aRest, fullArgs, fRest)
			}
		}
	}
}

// TestTreeConsistency walks commandTree recursively and checks that within
// each scope (the top level and each noun's children) every token — a
// command's Name or any of its Aliases, its own or a sibling's — resolves
// to exactly one command.
func TestTreeConsistency(t *testing.T) {
	var walk func(scope string, cmds []*command)
	walk = func(scope string, cmds []*command) {
		seen := make(map[string]string) // token -> command Name that owns it
		for _, c := range cmds {
			for _, token := range append([]string{c.Name}, c.Aliases...) {
				if owner, dup := seen[token]; dup {
					t.Errorf("%s: token %q is claimed by both %q and %q",
						scope, token, owner, c.Name)
				} else {
					seen[token] = c.Name
				}
			}
		}
		for _, c := range cmds {
			if len(c.Children) > 0 {
				walk(c.Name, c.Children)
			}
		}
	}
	walk("(top)", commandTree)
}

// TestDestructiveCommandsHaveNoAliases checks that the destructive
// commands (models delete, top-level uninstall) keep no short forms: their
// Aliases are empty, no alias in their scope resolves to them, and the
// full word still dispatches (delete must stay full-word, not disappear).
func TestDestructiveCommandsHaveNoAliases(t *testing.T) {
	find := func(cmds []*command, name string) *command {
		for _, c := range cmds {
			if c.Name == name {
				return c
			}
		}
		t.Fatalf("command %q not found in %v", name, commandNames(cmds))
		return nil
	}

	models := find(commandTree, "models")
	del := find(models.Children, "delete")
	if len(del.Aliases) != 0 {
		t.Errorf("models delete Aliases = %v, want empty", del.Aliases)
	}
	if got := find(commandTree, "uninstall"); len(got.Aliases) != 0 {
		t.Errorf("uninstall Aliases = %v, want empty", got.Aliases)
	}

	// No alias in models' scope may resolve to delete: it is destructive
	// and must stay full-word. ("delete" itself is the canonical Name, so
	// `m delete x` keeps working via the full word.)
	for _, c := range models.Children {
		for _, a := range c.Aliases {
			path, _, _, err := resolve(append(append([]string{"m"}, a), "x"))
			if err == nil && path == "models.delete" {
				t.Errorf("alias %q resolves to models.delete, want no alias for delete", a)
			}
		}
	}

	// The full word still dispatches, even through the alias parent.
	path, rest, wizard, err := resolve([]string{"m", "delete", "x"})
	if err != nil {
		t.Fatalf("resolve([m delete x]): unexpected error: %v", err)
	}
	if wizard {
		t.Error("resolve([m delete x]): wizard = true, want false")
	}
	if path != "models.delete" {
		t.Errorf("resolve([m delete x]): path = %q, want %q", path, "models.delete")
	}
	if !sameStrings(rest, []string{"x"}) {
		t.Errorf("resolve([m delete x]): rest = %v, want [x]", rest)
	}

	// A short form for delete is not accepted: unknown subcommand.
	_, _, _, err = resolve([]string{"m", "del", "x"})
	if err == nil {
		t.Error("resolve([m del x]): expected unknown-subcommand error, got nil")
	}
}

// TestAliasTableMatchesTree guards aliasPairs against drift from the
// Aliases fields in tree.go: the two must name exactly the same
// (alias, full) pairs.
func TestAliasTableMatchesTree(t *testing.T) {
	want := make(map[string]bool)
	var collect func(cmds []*command)
	collect = func(cmds []*command) {
		for _, c := range cmds {
			for _, a := range c.Aliases {
				want[a+"\x00"+c.Name] = true
			}
			collect(c.Children)
		}
	}
	collect(commandTree)

	got := make(map[string]bool, len(aliasPairs))
	for _, p := range aliasPairs {
		got[p.alias+"\x00"+p.full] = true
	}
	if !reflect.DeepEqual(want, got) {
		t.Errorf("aliasPairs does not match tree.go Aliases:\nwant %v\ngot  %v", want, got)
	}
}
