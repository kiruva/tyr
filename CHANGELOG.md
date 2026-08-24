# Changelog

All notable changes to tyr are documented in this file.

The format follows [Keep a Changelog](https://keepachangelog.com/en/1.1.0/), and the project
adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html). Versions are derived
from the commit history by GitVersion and tagged by CI; see
[CONTRIBUTING.md](CONTRIBUTING.md#releasing).

## [Unreleased]

### Changed

- **Renamed the project from `lazyfiles` to `tyr`.**
  - The binary is now `tyr`; the Go module path is `github.com/kiruva/tyr`.
  - The config directory moved from `$XDG_CONFIG_HOME/lazyfiles` (or `~/.config/lazyfiles`)
    to `$XDG_CONFIG_HOME/tyr` (or `~/.config/tyr`). Saved themes and connections carry over
    on their own — see below.
  - `LAZYFILES_THEME` is now `TYR_THEME`, and the ssh integration-test variables
    `LAZYFILES_TEST_SSH` and `LAZYFILES_TEST_SSH_KEY` are now `TYR_TEST_SSH` and
    `TYR_TEST_SSH_KEY`. Unlike the config directory, the old names are no longer read —
    update whatever exports them.

### Added

- **Pack dialog** (`p`): pick the format, the compression level and a password before anything
  runs, instead of always writing a default `.tar.gz`.
  - Formats: `tar.gz`, `tar.bz2`, `tar.xz`, `tar.zst`, `tar`, `zip`, `7z`. Only the formats
    whose tools are installed are offered.
  - Compression level per format (gzip/bzip2 `1`–`9`, xz `0`–`9`, zstd `1`–`19`, zip and 7z
    `0` store to `9`), passed to the compressor rather than ignored: the tar formats now stream
    `tar -cvf - | <compressor>`, which keeps the per-file progress bar.
  - The output name is prefilled and its extension follows the format until you type your own;
    `Enter` goes on to the usual confirm prompt, which restates format, level and encryption.
- **7-Zip is found under any of its names** — `7z` (p7zip), `7zz` (the official build) or
  `7za` — for packing, unpacking, encryption and the capabilities overlay, which names the
  command it actually found. 7-Zip also stands in for Info-ZIP now: a `.zip` is created and
  extracted with it when `zip`/`unzip` are not installed.
- **Password-protected archives.**
  - Packing a `.7z` or `.zip` with a password uses **AES-256** through `7z` when it is
    installed — and in a `.7z` the file names are encrypted too. Without `7z`, a `.zip` falls
    back to Info-ZIP's legacy ZipCrypto, which the dialog and confirm prompt both flag as weak.
  - Unpacking an encrypted archive prompts for the password when the tool reports one is
    needed, then runs the same job again; a wrong password says so and asks again. Encrypted
    `.zip` files are extracted with `7z` where available, since `unzip` cannot read AES entries.
  - Passwords are never written to disk or into the config, and are dropped when the dialog or
    prompt closes. `7z` is fed the password on stdin while packing, so it stays out of the
    process list; `zip -P`, `unzip -P` and `7z x -p…` take it as an argument, which is visible
    to other local users while the tool runs.
- **Capabilities overlay** (`C`): every archive, remote and local action with the tool it runs
  and whether that tool is on `PATH` — `✓` available, `✗` missing (with the binary named and
  what to install), `~` an optional per-format compressor that only matters if your `tar`
  shells out. The header counts what is unavailable. `?` and `C` swap between the keybindings
  and the capabilities; `PATH` is probed each time the overlay opens, so installing a tool
  takes effect without restarting tyr.
- **Rename.** `r` (or `F2`) renames the highlighted entry through a prefilled prompt.
- **Multi-rename tool** (`M`), a full-screen batch rename over the pane's selection:
  - find/replace read as a Go regex with `$1` backreferences, as literal text, or as an
    anchored glob (`ctrl+o` cycles; `alt+i` matches case-insensitively);
  - tokens in the `name`, `ext` **and `replace`** fields — `[N]`, `[N2-5]`, `[E]`, `[C]`/`[C3]`
    counter with a start and step (the step may be negative), `[P]` parent directory,
    `[d]`/`[t]` the file's own date and time. In `replace` they sit alongside `$1`
    backreferences, so `holiday_[d]_$1` is one replacement, and a token value containing a `$`
    is escaped rather than read as a group reference;
  - case conversion (`ctrl+t`) and whitespace trimming (`ctrl+p`), applied to the name without
    its extension;
  - recursive by default (`ctrl+r`), with directory names renamed only when asked (`ctrl+y`);
  - a live preview of every `old → new` pair, with clashes marked and held back, and a confirm
    prompt before anything moves. The batch runs off the UI thread with a progress bar.
  - Nothing is renamed over an existing name; swaps, chains and case-only changes are staged
    through temporary names, and children are renamed before their directory.
  - Local only for now: an ssh pane or an archive is refused with the reason in the status line.
- **Undo for renames** (`ctrl+z`), covering both the single rename and the batch tool. The
  history is per session, up to 20 batches deep, and walks back newest first; each undo asks
  for confirmation like any other operation. What is recorded is what the engine reported
  doing, so a batch that stopped part-way is still undoable, and a directory batch is taken
  apart in the opposite order it went together. A name that has moved on since — renamed by
  hand, or its old name taken again — is reported and skipped rather than forced; a batch
  where nothing can be put back is dropped from the history instead of blocking the ones under
  it. There is no redo.
- A one-time config migration on startup: if a pre-rename `lazyfiles` config directory is
  present and no `tyr` one is, the old directory is moved into place and the status bar says
  where it went. The move is skipped entirely when a `tyr` config already exists, so it can
  never overwrite settings you have under the new name, and it is a no-op on every run after
  the first. A migration that fails is reported but does not stop the app from starting.

## [0.1.0] - 2026-08-18

First public release.

### Added

**Navigation**

- Dual-pane layout with an active-pane border, `Tab` to switch, and `Enter`/`h` to walk the
  tree. Copy, move, pack, and unpack always run from the active pane to the other one.
- Cursor movement with `j`/`k` and the arrow keys, paging with `PgUp`/`PgDn`, and `g`/`G` to
  jump to the top or bottom of a listing.
- Multi-selection with `Space`, sort modes (name, size, time) on `s`, and a hidden-file
  toggle on `.`.
- A status bar reporting the location, item count, selection count, sort mode, and hidden-file
  state.
- An address bar on each pane's top line: `Ctrl+L` (or `:`) turns it into an input accepting
  absolute, relative, `~`-rooted, and `$VAR`-containing paths, with `Tab` completion.

**File operations**

- Recursive copy (`F5`/`c`), move (`F6`/`m`), and delete (`F8`/`Del`/`d`), each with a
  confirmation dialog that warns before overwriting and a live progress bar.
- Every operation runs off the UI thread and streams progress back as messages, so the
  interface stays responsive during large transfers. Both panes refresh on completion.
- New file (`n`) and new folder (`N`/`F7`) prompts, working on local and remote panes. Names
  may contain separators — missing parent directories are created — while absolute paths and
  names that already exist are refused rather than overwritten.

**Archives**

- Pack a selection to `.tar.gz` (`p`), unpack into the other pane (`u`), and unpack in place
  (`U`), all shelling out to `tar`, `unzip`, `7z`, or `unrar` — only the tool for the format
  in use is required.
- Browse an archive as a virtual directory tree with `Enter`, including per-member sizes.
- Copy or move real files into an open archive with `F5`/`c` or `F6`/`m`, adding them as
  members at the current virtual directory.

**Viewing and editing**

- A read-only pager (`v`) and a nano-style editor (`e`) with `Ctrl+S` to save, `Ctrl+Q` to
  discard, and an unsaved-changes guard on `Esc`. Binary files are refused.
- Both work on archive members: an edited member is written back with a targeted update for
  zip and uncompressed tar, and a transparent repack for compressed tar.

**Remote browsing over ssh**

- A connection modal (`S`) managing saved connections — name, host, user, port, starting
  path, and optional key file. Passwords are never written to disk.
- Authentication via ssh-agent, `~/.ssh/id_*`, or an interactive password prompt, honouring
  `~/.ssh/config` aliases including `ProxyJump`. Unrecognised host keys are shown for
  confirmation before being added to `known_hosts`.
- Remote targets in the address bar (`ssh://user@host/path` or `host:/path`), with `local:`
  as the way back to this machine.
- Transfers in both directions and within a single host: download, upload, server-side
  copy/move, and remote delete, streamed over one ssh session with per-file progress.

**Appearance and CLI**

- Eight built-in themes with a live-preview picker (`t`), selectable at startup with
  `--theme` and listable with `--themes`.
- A keybinding overlay (`?`) generated from the keymap, and a `--version` flag.

### Documentation

- README covering installation, keybindings, the address bar, archives, ssh, and themes.
- `CONTRIBUTING.md`, MIT license, and a `demo.tape` script for regenerating the demo GIF.

### Infrastructure

- GitHub Actions CI running `go vet`, `go build`, `go test`, and golangci-lint.
- GoReleaser workflow publishing Linux and macOS binaries (amd64 and arm64) per tag.
- A `Makefile` wrapping the common build, test, lint, and run targets.
