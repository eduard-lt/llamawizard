# Testing llamawizard configuration and service management

Run on macOS with Go installed:

```bash
go test ./... -race -count=1
go vet ./...
golangci-lint run ./...
```

The default suite uses a fake Homebrew executable and local HTTP fixtures. It does not install packages. LaunchAgent lifecycle tests are opt-in and use a dedicated `com.local.llamawizard-test` label:

```bash
LLAMAWIZARD_LAUNCHD_TESTS=1 go test ./internal/launchd -run 'TestLifecycle_RoundTrip|TestLoaded_TracksInstallAndStop' -count=1
```

The suite still includes read-only macOS hardware detection. Sandboxed environments may block sysctl or binding test sockets.

## Regression coverage

- Pi selector layout at 60/80/160/200 columns, scrolling, failed-health retries, and back navigation in model-add mode.
- Help flags on nested and destructive commands return help without modifying files or operating the service.
- Existing YAML profiles, comments, unknown options, aliases, and explicit context sizes survive adding a model.
- Pi preserves unknown fields, other providers, Pi-only models, defaults, and enabled-model patterns.
- Custom credentials and the chosen port/binary paths are saved consistently during setup.
- Enter-through navigation of the full setup wizard preserves an existing remote Pi default, custom YAML profiles, aliases, and advanced Pi fields; the test executes final config writes, fake launchctl operations, and a local authenticated API health check.
- Invalid YAML/JSON and duplicate aliases prevent updates.
- Concurrent edits abort a prepared commit; a failed write rolls back prior replacements.
- Model directory traversal and symlinks are rejected; shared model files cannot be deleted while another profile references them.
- Split GGUF downloads are explicitly rejected; nested paths and resume range validation are tested.
- Restart handles loaded/stopped services and verifies API readiness rather than trusting launchctl's exit code.

`TestCLIConfigLifecycle` compiles and runs the CLI with a temporary home, a fake launchctl, and a local authenticated API. It covers preview, apply, shared-file deletion refusal, model removal, direct URL addition, and a nonzero exit when the service rejects authentication. This exercises the command and filesystem flow without downloading real LLM weights or modifying the installed LaunchAgent.

## Manual acceptance before 0.1.8

1. Keep a copy of the current master YAML and Pi files.
2. Run `llamawizard version` and confirm the development build marker.
3. Run `config apply --dry-run`; inspect the planned file list.
4. Apply; verify that your custom profiles and context sizes appear in Pi.
5. Add a small single-file GGUF using `models add --link`; verify the API remains online and your previous profiles are intact.
6. Send a real inference request through one base model and one custom profile. API listing checks alone cannot establish inference correctness or memory fit.
7. Only after acceptance, merge the improvement branch and release 0.1.8 using the documented release workflow.
