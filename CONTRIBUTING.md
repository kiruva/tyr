# Contributing to tyr

Thanks for your interest! tyr aims to be a clean, keyboard-driven, dual-pane
file manager. Contributions of all sizes are welcome.

## Getting started

Go 1.26 or newer is required.

```sh
git clone https://github.com/kiruva/tyr
cd tyr
go run .
```

## Before opening a PR

Please make sure the following pass locally:

```sh
make fmt    # gofmt
make vet    # go vet ./...
make test   # go test ./...
make lint   # golangci-lint run (see .golangci.yml)
```

CI runs the same checks on every push and pull request.

## Project layout

| Path               | Responsibility                                           |
| ------------------ | -------------------------------------------------------- |
| `main.go`          | Entry point; starts the Bubble Tea program.              |
| `internal/app`     | Root model, update router, view, keymap (The Elm Arch.). |
| `internal/pane`    | One directory pane; also the virtual archive browser.    |
| `internal/fileops` | UI-agnostic create/copy/move/delete + archive engine.    |
| `internal/rename`  | UI-agnostic batch-rename planner and applier.            |
| `internal/remote`  | In-process ssh: listings, transfers, `~/.ssh/config`.    |
| `internal/config`  | The `key = value` config file: theme, saved connections. |
| `internal/ui`      | Lip Gloss theme and shared styles.                       |

## Guidelines

- Keep the `fileops` package free of any Bubble Tea / UI imports — it streams
  progress over a channel that the app layer adapts into messages.
- Filesystem operations must stay off the UI thread (run as a `tea.Cmd`).
- Match the surrounding style: small functions, clear names, comments where the
  _why_ isn't obvious.
- New keybindings go in `internal/app/keys.go` and should appear in the `?` help
  overlay automatically via `keyMap.groups()`.
- Anything remote must stay off the UI thread too: the pane records where it wants
  to be and the app layer fills it in from a `tea.Cmd`.
- The rename recipes in `MAN.md` are pinned by `TestDocumentedExamples` in
  `internal/rename/examples_test.go`. Change one and the other has to follow.
- Anything platform-dependent goes in a `_unix.go` / `_windows.go` pair behind one
  neutral function, rather than a `runtime.GOOS` branch in the middle of the logic.
  `make crossbuild` compiles every released target and is the cheap way to catch a
  pair that does not build.
- Note user-facing changes in `CHANGELOG.md` under `## [Unreleased]`.

## Commit messages

Conventional-commit subjects — `feat:`, `fix:`, `chore:`, `docs:`, `ci:`, and a `!`
suffix for a breaking change. They are not decoration: the release version is
derived from them (see below).

## Releasing

No version number is stored in the repo. GitVersion derives it from the commits
since the last tag, configured in [`GitVersion.yml`](GitVersion.yml):

| Commit subject                        | Bump   | Example       |
| ------------------------------------- | ------ | ------------- |
| `feat!: …`, or `BREAKING CHANGE` body | major  | 0.3.1 → 1.0.0 |
| `feat: …`                             | minor  | 0.3.1 → 0.4.0 |
| `fix:`, `chore:`, `docs:`, anything   | patch  | 0.3.1 → 0.3.2 |
| `+semver: major\|minor\|patch`        | forced | escape hatch  |

The largest bump among the commits since the last tag wins.

To cut a release:

1. Run the **Tag** workflow from the Actions tab with **Dry run** ticked. The summary
   reports the version it would use.
2. Move the `## [Unreleased]` heading in `CHANGELOG.md` to that version with today's
   date, and land it.
3. Run **Tag** again without dry run. It pushes an annotated `vX.Y.Z` and calls the
   **Release** workflow, which builds the binaries with GoReleaser and publishes them.

A tag pushed by CI cannot start another workflow, which is why **Tag** calls
**Release** directly instead of leaving it to the tag trigger. Pushing a tag by hand
still triggers **Release** on its own.

## Reporting bugs

Open an issue with your OS, terminal, tyr version (`tyr --version`),
and steps to reproduce.
