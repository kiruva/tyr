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

- **Docs split in two.** `README.md` is now a short tour for people deciding whether to use
  tyr; the full reference — every key, dialog, rename recipe, archive and ssh detail, and config
  line — moved to `MAN.md`.

### Added

- **Windows is a supported platform.** Releases now carry `windows/amd64` and `windows/arm64`
  binaries as a `.zip`, and CI builds and tests on Windows, macOS and Linux. Along with it:
  the config file lives at `%AppData%\tyr\config`, the trash at `%LocalAppData%\tyr\Trash`
  (tyr's own, with the same origin records, since the Recycle Bin cannot be restored from by
  `Ctrl+Z`), `!` opens `%COMSPEC%` and `x` runs through `cmd /C` with cmd's quoting. A drive
  letter in the address bar is no longer mistaken for an ssh host, a cross-volume move is
  detected by the error Windows actually returns, a recursive batch rename orders itself by
  depth whichever separator the paths carry, `.zip` extraction goes to 7-Zip rather than the
  Unix `unzip` that Git for Windows ships, and the properties dialog leaves out the owner and
  link rows that platform has no answer for. See [On Windows](MAN.md#on-windows).

- **The viewer got the four things a pager is asked for.** `/` finds text (with `n` / `N`
  walking the hits and wrapping around the file), `w` wraps long lines, `#` numbers them, and
  `x` shows the hex dump — offset, bytes, and the characters they stand for. `s` toggles
  syntax highlighting. Everything is rendered from the bytes the viewer holds, so each switch
  is a re-render: **a binary file now opens in hex instead of being refused.** A command run
  with `x` shows its output in the same pager.
- **Syntax highlighting**, built in rather than a dependency: four shapes of language —
  C-like, hash-comment (shell, Python, YAML, TOML, Makefiles), JSON and Markdown — with
  comments, strings, numbers and keywords found, and block comments, raw strings and fenced
  code blocks carried across the lines they span. Colours come from the same eight theme
  colours as the rest of the UI, so no theme has to say anything about code.
- **Your own themes**: a `.theme` file in `<config>/themes` — eight colours and an optional
  name — is in the picker on the next start, and a file named after a built-in replaces it.
  `--new-theme NAME` writes one from the theme you are using. A broken theme file is reported
  in the status bar and costs only its own theme.
- **Every key is rebindable.** `K` opens a key editor: `Enter` waits for the key to bind, `a`
  adds a second one, `d` resets one action and `D` resets all of them. A key that is already
  taken is refused and told whose it is; `Ctrl+C` and `Esc` cannot be rebound. Changes are
  written to the config as `key.<action> = …` lines as they are made, and the same lines can
  be written by hand. The help overlay is generated from the current bindings, so it cannot
  describe a key you have changed.
- **Mouse support**: click to focus a pane and move the cursor, double-click to open,
  right-click to mark, click the path to edit it, and the wheel to scroll whatever is under
  the pointer — including the lists inside dialogs. `mouse = off` in the config leaves the
  pointer to the terminal.

### Changed

- The help overlay's key labels are generated from what the keys are bound to rather than
  written by hand, so they follow a rebinding.
- `--help` lists `--new-theme`, and `--themes` includes themes loaded from the config
  directory.

- **A collision asks instead of clobbering.** A copy or move onto a name already in use stops
  and shows both files, with the newer one marked: overwrite, skip, keep both (the incoming
  file becomes `name (2).ext`), or overwrite only when the source is newer. The capital of each
  key answers the same way for every remaining collision, and `Esc` cancels the job — what it
  had already copied stays, and is still undoable. Two directories of the same name merge, and
  the question is asked file by file inside them.
- **Delete goes to the trash** — `$XDG_DATA_HOME/Trash` with a `.trashinfo` record on Linux,
  `~/.Trash` on macOS — and `Ctrl+Z` puts it back. `D` (or `Shift+F8`) is the permanent delete,
  and `delete = remove` in the config makes `F8` that one. Trashing across a filesystem
  boundary is refused rather than silently turned into a copy.
- **Undo covers more than renames**: a move goes back where it came from, a copy has the copies
  it made removed (and only those — a copy that overwrote something records nothing), a trashed
  delete is restored, and a rename still renames back. Twenty operations deep, in memory, for
  the session.
- **Compare & synchronize** (`F9` / `Y`): pairs the two panes' trees recursively and lists what
  differs, with a direction proposed for every row — missing files go across, the newer side
  wins. `→`/`←` change a row, `s` skips it, `a` restores the proposal, `e` shows the matching
  files, `.` includes dotfiles and `c` compares byte for byte instead of by timestamp. `Enter`
  states how many copies go each way before anything is overwritten.
- **Properties** (`i`): type, size, modification time, owner and group, hard links, symlink
  target, and the mode — with the permissions editable as octal, recursively for a directory,
  run as a normal job with a progress bar. Symlinks are left alone.
- **Bookmarks**: `B` saves the directory the pane is in, `b` lists them, `Enter` goes there and
  `d` removes one. They are one `bookmark.<name> = <path>` line each in the config file.
- **Shell integration**: `!` hands the terminal to `$SHELL` in the active pane's directory and
  refreshes the pane when it exits; `x` runs one command line there and shows the output in the
  pager, with `%f`, `%F`, `%s`, `%d` and `%D` standing in for the pane's state — each shell
  quoted — and `Esc` to cancel. `--cd-file PATH` (or `$TYR_CD_FILE`) writes the directory tyr
  exits in, so a shell wrapper can follow it there.
- `--help` prints the flags.

### Changed

- The help and capabilities overlays scroll (`↑`/`↓`, `PgUp`/`PgDn`) when the terminal is too
  short to hold them; any other key still closes them.

- **Filter** (`/`): narrows the active pane as you type — glob (`*.go`) or substring, both
  case-insensitive. `Enter` keeps it, `Esc` in the prompt restores the previous one, and `Esc`
  with nothing else open clears it. The filter survives a refresh, is dropped when the pane
  moves, and bounds everything that acts on "what is visible".
- **Find** (`Ctrl+F` / `F`): searches the active pane's directory and everything below it by
  name and, optionally, by text inside the files, with switches for case and hidden files. The
  walk runs off the UI thread and `Esc` cancels it; `Enter` on a hit takes the pane to the file
  with the cursor on it, `/` reopens the query to narrow it. Content matches report the line
  they landed on; binary files and anything over 32 MB are matched by name only. Results stop
  at 500 hits and say so. Local panes only.
- **Selection by the handful**: `Ctrl+A` selects everything visible, `*` inverts the selection,
  and `+` / `-` mark and unmark by mask (`*.go;*.md` — globs or substrings, `;` separated). All
  four act on what the pane is showing, so a filter bounds them.
- **Directory sizes**: `Space` on a directory measures what is inside it while it marks it, and
  `=` measures every directory in the pane. The walks run off the UI thread, the size column
  fills in as totals land, and a pane sorted by size reorders with the cursor following its
  entry. Totals are kept until the pane moves; measuring is local-only.
- **Refresh** (`Ctrl+R` / `R`): re-reads the active pane, over ssh when it is remote.
- **The session is remembered**: quitting writes each pane's sort order, hidden-file setting
  and directory to the config file. Sort and hidden come back on the next run; the directories
  come back only with `startup = last` in the config, and a directory that has gone away is
  skipped. A pane left on a remote host saves no path.

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

### Infrastructure

- **Nightly prereleases.** A scheduled workflow builds every released target from `main` once
  a day and publishes them under a rolling `nightly` tag. It compares `main` against the
  commit the last nightly was built from and does nothing when they are the same, so a quiet
  week costs one skipped job a day rather than seven identical prereleases.
- **The changelog cuts the release.** Tagging now promotes `## [Unreleased]` to
  `## [X.Y.Z] - <date>` itself, commits that, and tags the commit it made; the release notes
  GitHub shows are that section, read out of `CHANGELOG.md` by `scripts/changelog.sh` rather
  than assembled from commit subjects. The dry run prints the notes the release would carry.
  The commit reaches `main` past its ruleset with a write deploy key, since Actions itself
  cannot be a bypass actor on a user-owned repository — see
  [CONTRIBUTING.md](CONTRIBUTING.md#how-the-bot-gets-past-the-ruleset).

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
