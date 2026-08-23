### Features

- Add `warlock` subcommand for live dashboard and service monitoring (da3beaa, cc7f222, 8d3d761)
- Add LAN access option to `warlock` dashboard (da3beaa)

### Bug Fixes

- Improve `warlock` LAN access handling and signal management (40d09f5)
- Fix `warlock` restart and stop behavior (ab9baa2, 3202ee5)
- Fix `warlock` layout and terminal title (323a642, 3a4d44e)
- Fix `vm_stat` parsing on macOS (1b36f3d)
- Resolve `golangci-lint` issues (cbb67d4)
- Fix flaky connection race in tests (60d1bb0)

### Refactors

- Simplify `warlock` dashboard by removing GPU/CPU and sudo support (4d77808)
- Extract welcome-screen rain into `internal/ripple` package (985bc08)

### Documentation

- Update `warlock` documentation regarding listen host (4983180)

### Chore

- Probe egress IP via 8.8.8.8:53 for `warlock` (5413959)


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

