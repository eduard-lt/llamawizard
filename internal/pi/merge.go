package pi

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/eduard-lt/llamawizard/internal/state"
)

type object = map[string]json.RawMessage

func objectFrom(data []byte) (object, error) {
	obj := object{}
	if len(data) == 0 {
		return obj, nil
	}
	if err := json.Unmarshal(data, &obj); err != nil {
		return nil, err
	}
	if obj == nil {
		return nil, fmt.Errorf("expected JSON object, got null")
	}
	return obj, nil
}
func put(obj object, key string, value any) { obj[key], _ = json.Marshal(value) }

// RenderModels preserves every unknown provider/model field. The master owns
// local IDs, names, explicit context sizes and endpoint credentials only.
// Only explicitly removed IDs are pruned, so Pi-only custom entries survive.
func RenderModels(existing []byte, port int, key string, models []state.ModelEntry, remove []string) ([]byte, error) {
	root, err := objectFrom(existing)
	if err != nil {
		return nil, err
	}
	providers, err := objectFrom(root["providers"])
	if err != nil {
		return nil, err
	}
	local, err := objectFrom(providers["local"])
	if err != nil {
		return nil, err
	}
	var entries []object
	if b := local["models"]; len(b) > 0 {
		if err := json.Unmarshal(b, &entries); err != nil {
			return nil, err
		}
	}
	deleted := map[string]bool{}
	for _, id := range remove {
		deleted[id] = true
	}
	var result []object
	index := map[string]int{}
	for _, entry := range entries {
		var id string
		if err := json.Unmarshal(entry["id"], &id); err != nil || id == "" {
			return nil, fmt.Errorf("pi model requires an ID")
		}
		if deleted[id] {
			continue
		}
		if _, ok := index[id]; ok {
			return nil, fmt.Errorf("duplicate Pi model ID %s", id)
		}
		index[id] = len(result)
		result = append(result, entry)
	}
	for _, m := range models {
		entry := object{}
		i, ok := index[m.Slug]
		if ok {
			entry = result[i]
		}
		put(entry, "id", m.Slug)
		name := m.Name
		if name == "" {
			name = m.Slug
		}
		put(entry, "name", name)
		if m.CtxSize > 0 {
			put(entry, "contextWindow", m.CtxSize)
		}
		if !ok {
			index[m.Slug] = len(result)
			result = append(result, entry)
		}
	}
	if result == nil {
		result = []object{}
	}
	put(local, "api", "openai-completions")
	put(local, "baseUrl", fmt.Sprintf("http://127.0.0.1:%d/v1", port))
	if key == "" {
		key = "dummy"
	}
	key = strings.ReplaceAll(key, "$", "$$")
	if strings.HasPrefix(key, "!") {
		key = "$" + key
	}
	put(local, "apiKey", key)
	put(local, "models", result)
	put(providers, "local", local)
	put(root, "providers", providers)
	return json.MarshalIndent(root, "", "  ")
}

// An empty defaultModel preserves the user's default and enabledModels. An
// explicit choice changes the default only; existing selection patterns stay.
func RenderSettings(existing []byte, defaultModel string, models []state.ModelEntry) ([]byte, error) {
	root, err := objectFrom(existing)
	if err != nil {
		return nil, err
	}
	if defaultModel != "" {
		found := false
		for _, m := range models {
			if m.Slug == defaultModel {
				found = true
			}
		}
		if !found {
			return nil, fmt.Errorf("default model %q is absent from master config", defaultModel)
		}
		put(root, "defaultProvider", "local")
		put(root, "defaultModel", defaultModel)
	} else if len(root["defaultModel"]) == 0 && len(models) > 0 {
		put(root, "defaultProvider", "local")
		put(root, "defaultModel", models[0].Slug)
	}
	if _, ok := root["enabledModels"]; !ok {
		enabled := []string{}
		for _, m := range models {
			enabled = append(enabled, "local/"+m.Slug)
		}
		put(root, "enabledModels", enabled)
	}
	return json.MarshalIndent(root, "", "  ")
}
