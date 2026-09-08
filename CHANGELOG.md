# Changelog

All notable changes to this project will be documented in this file.

## Unreleased — planned 0.1.8

- Separate the current Pi default from local model choices, scroll long lists, and fit setup summaries to smaller terminals.
- Support `-help` and help flags on nested commands without executing them; show custom profiles in model listing and inspection.
- Retry failed service checks instead of showing setup complete, and clarify setup navigation and API-key preservation.
- Preserve edited llama-swap profiles, aliases, context sizes, and unknown YAML fields when adding models.
- Add `config apply` with preview, backups, Pi merging, and authenticated API readiness checks.
- Preserve Pi-only models, other providers, advanced options, and user settings; synchronize shared names, contexts, and keys.
- Save final setup port, credentials, and binary paths; avoid premature config writes.
- Restart loaded services with kickstart and bootstrap stopped services with retries.
- Reject unsafe model directories and deletion of model files referenced by another profile.
- Reject unsupported split GGUF downloads before registering an incomplete model; validate resume offsets and support nested paths.
- Remove automatic slug migrations from command startup.
- Replace real Homebrew installation in unit tests, isolate launchd integration tests, and add regression and CLI end-to-end coverage.
- Add customization, recovery, testing, and discoverability documentation.


### Bug Fixes

- Correct formatting in releasing documentation (675cf9a)
- Add plans directory to .gitignore (717ef78)

### Documentation

- Releasing and versioning tutorial (9122a40)
- Task-runner llama-swap model profile (128K ctx, 1 slot, temp 0.7) (d12dfd3)

### Features

- Show llama.cpp and llama-swap versions in version command (#3) (9c268f0)

### Features

- Overhaul models add --link and unify model slugs (#1) (948a2f0)

### Bug Fixes

- Remove doubled v prefix in version display (5b0e43f)

### Bug Fixes

- Check syscall.Exec return value for lint compliance (720d1dc)
- Update installation commands to use the correct repository path (5e88d3d)
- Improve update handling and restart mechanism (3d6c9b3)
