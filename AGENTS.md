# Repository working conventions

- Follow the existing branch naming: `feat/<description>` for features and `fix/<description>` for bug fixes. Do not use `codex/` or other agent names in branch names.
- Existing user configuration must survive both the full setup wizard and model-add flows. Pressing Enter through setup must preserve existing Pi defaults and model settings unless the user explicitly selects a change.
- Test complete wizard navigation as well as individual configuration functions. Use temporary homes and fake external commands; never use the user's service label or install packages in default tests.
- Merge and release only after the user accepts the local development build.
