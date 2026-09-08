package llamaswap

import (
	"strings"
	"testing"
)

func TestReferencesSharedModelDirectory(t *testing.T) {
	data := []byte("models:\n  base:\n    cmd: /server --model '/models/base/model.gguf'\n  persona:\n    cmd: /server --model '/models/base/model.gguf'\n")
	ids, err := References(data, "/models/base", "base")
	if err != nil || len(ids) != 1 || ids[0] != "persona" {
		t.Fatalf("%v %v", ids, err)
	}
}
func TestCatalogRejectsCollisionAndBrokenQuoting(t *testing.T) {
	for _, s := range []string{"models:\n  a:\n    cmd: /server\n    aliases: [b]\n  b:\n    cmd: /server\n", "models:\n  a:\n    cmd: /server 'broken\n", "models:\n  a:\n    cmd: /server\n  a:\n    cmd: /other\n"} {
		if _, _, err := Catalog([]byte(s)); err == nil {
			t.Fatalf("accepted %s", s)
		}
	}
}
func TestCommandWordsQuotedPaths(t *testing.T) {
	want := "/Users/it's me/models/model.gguf"
	words, err := CommandWords("/server --model " + shellQuote(want) + " --ctx-size=32768")
	if err != nil || len(words) != 4 || words[2] != want {
		t.Fatalf("%v %v", words, err)
	}
	out, err := Merge([]byte("models: {}\n"), []byte("models:\n  a:\n    cmd: /server --ctx-size=32768\n"), nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	entries, _, err := Catalog(out)
	if err != nil || entries[0].CtxSize != 32768 || !strings.Contains(string(out), "a:") {
		t.Fatalf("%v %v", entries, err)
	}
}
func TestListedModelsDoNotRequireHiddenAliases(t *testing.T) {
	data := []byte("models:\n  base:\n    cmd: /server\n    aliases: [fast]\n  secret:\n    cmd: /server\n    unlisted: true\n")
	ids, err := ListedModelIDs(data)
	if err != nil || len(ids) != 1 || ids[0] != "base" {
		t.Fatalf("%v %v", ids, err)
	}
	ids, err = ListedModelIDs(append([]byte("includeAliasesInList: true\n"), data...))
	if err != nil || len(ids) != 2 {
		t.Fatalf("%v %v", ids, err)
	}
}
func TestCatalogReadsContextMacros(t *testing.T) {
	data := []byte("macros:\n  ctx: 65536\n  flags: '--ctx-size ${ctx}'\nmodels:\n  base:\n    cmd: /server ${flags}\n")
	models, _, err := Catalog(data)
	if err != nil || models[0].CtxSize != 65536 {
		t.Fatalf("%v %v", models, err)
	}
}
