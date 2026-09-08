# Customize local LLM profiles without losing your settings

`~/.local/ai/config/llama-swap.yaml` is the master model configuration. Edit it to tune context sizes, GPU flags, cache types, aliases, or multiple profiles using the same GGUF weights. `state.json` tracks downloaded files and the service port; it no longer replaces existing model definitions.

## Edit, preview, apply

```bash
llamawizard config path
$EDITOR ~/.local/ai/config/llama-swap.yaml
llamawizard config apply --dry-run
llamawizard config apply
```

Preview validates YAML, duplicate IDs/aliases, command quoting, and the Pi JSON files when Pi integration is enabled. It lists the files that would change and does not write files or restart the service. Apply backs up changed files, writes the updates, restarts llama-swap, and verifies its authenticated `/v1/models` endpoint. An API readiness check does not load every model or guarantee inference fits in RAM.

## One model, multiple profiles

Keep a separate model entry when changing process arguments such as context size. An alias routes to the same process and cannot have a different context size on its own.

```yaml
apiKeys:
  - your-local-key

models:
  qwen-coding:
    name: Qwen coding
    cmd: |
      '/opt/homebrew/bin/llama-server'
      --host 127.0.0.1 --port ${PORT}
      --model '/Users/YOU/models/qwen/model.gguf'
      --ctx-size 98304
      --cache-type-k q8_0 --cache-type-v q8_0
      -ngl 999 --jinja
    aliases: [coder]

  corp-ceo:
    name: CEO profile
    cmd: |
      '/opt/homebrew/bin/llama-server'
      --host 127.0.0.1 --port ${PORT}
      --model '/Users/YOU/models/qwen/model.gguf'
      --ctx-size 65536
      -ngl 999 --jinja
```

Replace the example paths with your actual binary and model locations. A profile name does not create a persona/system prompt; configure that in your client. Larger contexts need more memory.

Simple scalar `macros` are supported when extracting context sizes for Pi, including model-level overrides. The original command and macros remain in the master file. Advanced llama-swap options are preserved, although the preview is not a full upstream schema validator.

## What is synchronized to Pi?

When Pi integration is enabled, apply merges into `~/.pi/agent/models.json` and `settings.json`:

| Setting | Behavior |
| --- | --- |
| Master model IDs and aliases | Available under Pi's `local` provider |
| Display names | Copied from the master |
| Explicit `--ctx-size`, `--ctx-size=`, or `-c` | Copied to `contextWindow` |
| API key | First master `apiKeys` entry; a placeholder if auth is disabled |
| API endpoint | Localhost and the saved service port |
| Pi-only models and other providers | Preserved |
| Pi `reasoning`, `maxTokens`, `compat`, costs, other fields | Preserved |
| Pi theme, compaction, enabled-model patterns, chosen default | Preserved |

Rerunning the full setup wizard preselects **Keep existing Pi settings**. Pressing Enter preserves your default, including a remote provider. Move to a model explicitly to change it.

Change Pi's default explicitly:

```bash
llamawizard config apply --default corp-ceo
```

If your `enabledModels` list restricts model cycling, update its patterns in Pi when needed. Preserving that list means a new model is not automatically added to a hand-curated cycle. Pi-specific settings remain in Pi; the YAML is the master for shared model settings, not a replacement for every Pi option.

## Adding and removing models

`models add` appends absent model entries. Existing commands, aliases, custom profiles, unknown YAML fields, and comments survive. Re-adding an installed slug is rejected before downloading over its files.

`models remove ID` removes that inventory entry and its config entry, retains files, syncs Pi, and checks service readiness after restart. `models delete ID` also removes its directory, but refuses if another profile still uses it. To remove a custom profile, delete its YAML entry and run `config apply`.

After the first apply, `~/.local/ai/last-applied.yaml` records which IDs came from the master. Subsequent applies can remove those IDs from Pi without deleting unrelated Pi-only entries. Do not edit this snapshot.

Existing slugs and directories are no longer automatically renamed when a command starts. Help, version, and status do not migrate model files.

## Backups and recovery

Changed files are backed up under `~/.local/ai/backups/<timestamp>-<unique suffix>/`. Each backup contains numbered original files and a `manifest.json` mapping them to their original paths. Backups and new config files are owner-readable only; they can contain API keys.

All output is prepared and validated before any replacement. A failed file write rolls back earlier replacements. Concurrent config edits detected before commit abort the update. A service startup failure returns an error and retains the saved config and backup so you can diagnose it; it does not claim the service is available or silently undo your edits.

To recover, copy the desired numbered backup files to the paths listed in the manifest, then run `config apply --dry-run` and `config apply`. Backups capture the files as they were immediately before apply; for manual YAML edits, `last-applied.yaml` can also help recover the previously applied model settings. A process or machine crash during a multi-file update may require manual recovery from the backup.

## Current download limit

Automatic split GGUF downloads are not supported in this version. Files such as `model-00001-of-00003.gguf` are rejected before downloading, rather than registering an incomplete model. Choose a single-file GGUF. Nested file paths are supported for direct Hugging Face resolve links.
