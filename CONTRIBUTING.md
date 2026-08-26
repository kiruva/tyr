# Contributing to tyr

Thanks for your interest! tyr aims to be a clean, keyboard-driven, dual-pane
file manager. Contributions of all sizes are welcome.

## Getting started

Go 1.26.6 or newer is required — 1.26.6 is the release that fixes an
`encoding/asn1` flaw tyr reaches when it parses an ssh private key.

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
   reports the version it would use and prints the release notes it would carry.
2. Run **Tag** again without dry run.

Step 2 does the rest by itself: it promotes `## [Unreleased]` in `CHANGELOG.md` to
`## [X.Y.Z] - <today>`, commits that as `docs(changelog): release vX.Y.Z`, pushes an
annotated `vX.Y.Z` at *that* commit, and calls the **Release** workflow, which builds
the binaries with GoReleaser and publishes them with the changelog section as the
release notes.

Two things to know about it:

- The changelog commit goes to the branch you dispatched from, which on `main` means
  getting past the ruleset (see below). A push that is refused fails the run before
  anything is tagged, which is the safe end to fail at.
- An empty `## [Unreleased]` is a warning, not a stop: the tag still goes out, and
  GoReleaser falls back to its own commit list for the notes. Write the entries as you
  land the changes and this never comes up.

### How the bot gets past the ruleset

`main` is guarded by a repository ruleset that requires a pull request. `GITHUB_TOKEN`
cannot be exempted from it: ruleset bypass takes an *integration*, and on a user-owned
repository the GitHub Actions app is not an installed integration — which is also why
`github-actions[bot]` never appears in the bypass picker. A write **deploy key** can be
exempted, so that is what pushes:

- The ruleset on `main` lists one bypass actor, `DeployKey` with mode `always`.
- The repository has a read-write deploy key, *release: changelog promotion*.
- Its private half is the Actions secret `CHANGELOG_DEPLOY_KEY`, which the **Tag**
  workflow hands to `actions/checkout` as `ssh-key`, so both the changelog commit and
  the tag are pushed over SSH as that key.

The consequence to keep in mind: any read-write deploy key on this repository can push
straight to `main`. Add one only for something that needs exactly that, and drop the
one above if this automation ever goes away. Rotating it is three commands:

```sh
ssh-keygen -t ed25519 -N "" -C "tyr release changelog (Actions)" -f ck
gh repo deploy-key add ck.pub --title "release: changelog promotion" --allow-write
gh secret set CHANGELOG_DEPLOY_KEY < ck && rm -f ck ck.pub   # then delete the old key
```

Nightly is unaffected by any of this: it tags and releases, and a ruleset scoped to a
branch has nothing to say about tags.

A tag pushed by CI cannot start another workflow, which is why **Tag** calls
**Release** directly instead of leaving it to the tag trigger. Pushing a tag by hand
still triggers **Release** on its own — and because the changelog is read from the
commit being built, a hand-pushed tag wants the heading moved by hand first.

`scripts/changelog.sh` is what both workflows call, and it runs locally too:

```sh
scripts/changelog.sh has-entries        # exit 0 if Unreleased says something
scripts/changelog.sh section unreleased # print a section, as the notes would read
scripts/changelog.sh promote 0.2.0      # move the heading, dated today (UTC)
```

### Nightly builds

A scheduled workflow builds `main` for every released target at 03:17 UTC and publishes
them as a prerelease under a rolling `nightly` tag. It first compares `main` with the
commit the previous nightly came from and stops there when nothing has landed, so an
unchanged week produces no new prereleases. Run **Nightly** by hand with **force**
ticked to rebuild the same commit anyway.

Nightly archives are versioned `X.Y.Z-nightly.<commit>`, they are not tagged releases,
and each one replaces the last.

## Reporting bugs

Open an issue with your OS, terminal, tyr version (`tyr --version`),
and steps to reproduce.
