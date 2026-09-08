package llamaswap

import (
	"fmt"
	"strconv"
	"strings"
	"unicode"

	"gopkg.in/yaml.v3"

	"github.com/eduard-lt/llamawizard/internal/state"
)

func mapping(n *yaml.Node, key string) *yaml.Node {
	for i := 0; i+1 < len(n.Content); i += 2 {
		if n.Content[i].Value == key {
			return n.Content[i+1]
		}
	}
	return nil
}

func document(data []byte) (*yaml.Node, error) {
	var n yaml.Node
	if err := yaml.Unmarshal(data, &n); err != nil {
		return nil, err
	}
	if len(n.Content) != 1 || n.Content[0].Kind != yaml.MappingNode {
		return nil, fmt.Errorf("config must be a YAML mapping")
	}
	// Decode as well to detect duplicate mapping keys.
	var check map[string]any
	if err := n.Decode(&check); err != nil {
		return nil, err
	}
	models := mapping(n.Content[0], "models")
	if models == nil || models.Kind != yaml.MappingNode {
		return nil, fmt.Errorf("config requires a models mapping")
	}
	return &n, nil
}

// Merge retains existing model nodes, comments and unknown options. Only absent
// IDs are appended. Deletion and authentication changes must be explicit.
func Merge(existing, generated []byte, remove []string, apiKey *string) ([]byte, error) {
	if len(existing) == 0 {
		existing = generated
	}
	doc, err := document(existing)
	if err != nil {
		return nil, err
	}
	gen, err := document(generated)
	if err != nil {
		return nil, err
	}
	root := doc.Content[0]
	models := mapping(root, "models")
	deleted := map[string]bool{}
	for _, id := range remove {
		deleted[id] = true
	}
	var kept []*yaml.Node
	for i := 0; i < len(models.Content); i += 2 {
		if !deleted[models.Content[i].Value] {
			kept = append(kept, models.Content[i:i+2]...)
		}
	}
	models.Content = kept
	added := mapping(gen.Content[0], "models")
	for i := 0; i < len(added.Content); i += 2 {
		id := added.Content[i].Value
		if !deleted[id] && mapping(models, id) == nil {
			models.Content = append(models.Content, added.Content[i:i+2]...)
		}
	}
	if apiKey != nil {
		keys := mapping(root, "apiKeys")
		if keys == nil {
			keys = &yaml.Node{}
			root.Content = append(root.Content, &yaml.Node{Kind: yaml.ScalarNode, Value: "apiKeys"}, keys)
		}
		*keys = yaml.Node{Kind: yaml.SequenceNode, Tag: "!!seq"}
		if *apiKey != "" {
			keys.Content = append(keys.Content, &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: *apiKey})
		}
	}
	out, err := yaml.Marshal(doc)
	if err != nil {
		return nil, err
	}
	if _, _, err = Catalog(out); err != nil {
		return nil, err
	}
	return out, nil
}

// Catalog reads the master config, including profiles unknown to state.json.
// Aliases are exposed to Pi as selectable model IDs sharing the same settings.
func Catalog(data []byte) ([]state.ModelEntry, string, error) {
	doc, err := document(data)
	if err != nil {
		return nil, "", err
	}
	if err = ValidateConfigYAML(data); err != nil {
		return nil, "", err
	}
	root := doc.Content[0]
	models := mapping(root, "models")
	key := ""
	if keys := mapping(root, "apiKeys"); keys != nil {
		var ks []string
		if err := keys.Decode(&ks); err != nil {
			return nil, "", err
		}
		if len(ks) > 0 {
			key = ks[0]
		}
	}
	var result []state.ModelEntry
	seen := map[string]bool{}
	for i := 0; i < len(models.Content); i += 2 {
		id := models.Content[i].Value
		var cfg modelConfig
		if err := models.Content[i+1].Decode(&cfg); err != nil {
			return nil, "", err
		}
		words, err := CommandWords(expandMacros(string(cfg.Cmd), root, models.Content[i+1]))
		if err != nil {
			return nil, "", fmt.Errorf("model %s: %w", id, err)
		}
		ctx := 0
		for j, w := range words {
			value := ""
			if (w == "--ctx-size" || w == "-c") && j+1 < len(words) {
				value = words[j+1]
			} else if strings.HasPrefix(w, "--ctx-size=") {
				value = strings.TrimPrefix(w, "--ctx-size=")
			}
			if value != "" {
				ctx, err = strconv.Atoi(value)
				if err != nil || ctx < 0 {
					return nil, "", fmt.Errorf("model %s: invalid context size %q", id, value)
				}
			}
		}
		for _, modelID := range append([]string{id}, cfg.Aliases...) {
			if strings.TrimSpace(modelID) == "" || seen[modelID] {
				return nil, "", fmt.Errorf("empty or duplicate model/alias ID %q", modelID)
			}
			seen[modelID] = true
			name := cfg.Name
			if name == "" {
				name = id
			}
			if modelID != id {
				name = modelID + " (" + name + ")"
			}
			result = append(result, state.ModelEntry{Slug: modelID, Name: name, CtxSize: ctx})
		}
	}
	return result, key, nil
}

// CommandWords handles POSIX quoting without executing or expanding anything.
func CommandWords(cmd string) ([]string, error) {
	var words []string
	var b strings.Builder
	var quote rune
	escape, started := false, false
	flush := func() {
		if started {
			words = append(words, b.String())
			b.Reset()
			started = false
		}
	}
	for _, r := range cmd {
		if escape {
			if r != '\n' {
				b.WriteRune(r)
			}
			escape = false
			continue
		}
		if r == '\\' && quote != '\'' {
			escape = true
			started = true
			continue
		}
		if quote != 0 {
			if r == quote {
				quote = 0
			} else {
				b.WriteRune(r)
			}
			continue
		}
		if r == '\'' || r == '"' {
			quote = r
			started = true
			continue
		}
		if unicode.IsSpace(r) {
			flush()
			continue
		}
		started = true
		b.WriteRune(r)
	}
	if quote != 0 || escape {
		return nil, fmt.Errorf("unfinished quoting in command")
	}
	flush()
	return words, nil
}

// References returns other profiles that use files inside a model directory.
func References(data []byte, dir, excludedID string) ([]string, error) {
	doc, err := document(data)
	if err != nil {
		return nil, err
	}
	models := mapping(doc.Content[0], "models")
	var ids []string
	for i := 0; i < len(models.Content); i += 2 {
		id := models.Content[i].Value
		if id == excludedID {
			continue
		}
		var cfg modelConfig
		if err := models.Content[i+1].Decode(&cfg); err != nil {
			return nil, err
		}
		words, err := CommandWords(expandMacros(string(cfg.Cmd), doc.Content[0], models.Content[i+1]))
		if err != nil {
			return nil, err
		}
		for _, w := range words {
			if w == dir || strings.HasPrefix(w, dir+"/") || strings.Contains(w, "="+dir+"/") {
				ids = append(ids, id)
				break
			}
		}
	}
	return ids, nil
}

// ListedModelIDs respects llama-swap's listing policy: aliases are hidden by
// default and unlisted profiles must not make a readiness check fail.
func ListedModelIDs(data []byte) ([]string, error) {
	doc, err := document(data)
	if err != nil {
		return nil, err
	}
	root := doc.Content[0]
	include := false
	if n := mapping(root, "includeAliasesInList"); n != nil {
		if err = n.Decode(&include); err != nil {
			return nil, err
		}
	}
	models := mapping(root, "models")
	var ids []string
	for i := 0; i < len(models.Content); i += 2 {
		var cfg struct {
			Unlisted bool     `yaml:"unlisted"`
			Aliases  []string `yaml:"aliases"`
		}
		if err := models.Content[i+1].Decode(&cfg); err != nil {
			return nil, err
		}
		if cfg.Unlisted {
			continue
		}
		ids = append(ids, models.Content[i].Value)
		if include {
			ids = append(ids, cfg.Aliases...)
		}
	}
	return ids, nil
}

func expandMacros(cmd string, root, model *yaml.Node) string {
	values := map[string]string{}
	for _, node := range []*yaml.Node{mapping(root, "macros"), mapping(model, "macros")} {
		if node == nil {
			continue
		}
		for i := 0; i+1 < len(node.Content); i += 2 {
			if node.Content[i+1].Kind == yaml.ScalarNode {
				values[node.Content[i].Value] = node.Content[i+1].Value
			}
		}
	}
	for pass := 0; pass < 10; pass++ {
		before := cmd
		for key, value := range values {
			cmd = strings.ReplaceAll(cmd, "${"+key+"}", value)
		}
		if before == cmd {
			break
		}
	}
	return cmd
}
