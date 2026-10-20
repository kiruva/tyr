# Changelog

All notable changes to tyr are documented in this file.

The format follows [Keep a Changelog](https://keepachangelog.com/en/1.1.0/), and the project
adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html). Versions are derived
from the commit history by GitVersion and tagged by CI; see
[CONTRIBUTING.md](CONTRIBUTING.md#releasing).

## [Unreleased]

### Fixed

- **Saving from the editor no longer changes what you did not touch.** The editor turned every
  tab into four spaces, every `\r\n` into two line breaks, dropped other control characters and
  cut files at 10000 lines — so a Makefile saved unchanged stopped working. It is replaced by
  an editor of tyr's own that keeps a file as it found it: tabs stay tabs and are drawn as
  indentation, CRLF stays CRLF, control characters show as `^X`, and there is no line limit.
- **Enter works past line 99** in the editor, and a file no longer counts as changed the moment
  it is opened.
- **`e` in the viewer edits the file being viewed**, not whatever the pane's cursor is on — and
  does nothing on a command's output.
- **A cancelled search, command or comparison no longer lands on the one started after it.**
  Cancelling find, an `x` command or the compare tool and starting again straight away could
  show the old run's leftovers as the new result — "nothing matched", the killed command's
  output, or the compare tool closing itself — and left the new run impossible to stop with
  `Esc`.
- **Batch rename plans against the toggles as they are.** Flipping recursion off and on quickly
  could let a slower sweep land last, so subfolder files were renamed with recursion shown off.
  Closing the tool now also stops a sweep that is still walking.
- **A new file or folder made on a host is highlighted only where it was made** — not in
  whatever directory the pane had moved to by the time the host answered.
- **Operations act only on the marked entries you can see.** Mark five files, filter down to
  one, press F8: the four marked files the filter hid were deleted too. Marks hidden by a filter
  or by hiding dotfiles are now set aside — they are back when the filter is — and a mark on a
  file that has gone from the folder is forgotten.
- **Undoing a copy no longer deletes your work.** A copy edited since was deleted for good along
  with the rest. Copies that changed are now left alone, the prompt says how many, and the rest
  go to the trash rather than being unlinked (a delete for good is offered, and named as one,
  only when the trash cannot take them). Undoing a move likewise leaves a file that has since
  been replaced where it is, and an undo that is cancelled part-way keeps the rest on the
  history.
- **Changing permissions recursively no longer locks you out of the folder.** One mode went on
  files and folders alike, so `644` on a tree closed every folder in it and then failed reading
  the first one. Folders now keep execute wherever they can be read, and files keep it only if
  they had it — chmod's `X`. A recursive change asks first, and says what folders and files will
  each get.
- **Ctrl+C always quits**, even with quit rebound to another key, and quitting from a dialog
  now saves where the panes were and closes ssh sessions the way `q` does.
- **CJK and other wide names fit their column.** Names, paths and wrapped viewer lines were cut
  by character count, so a Japanese file name took twice its room and pushed the size off the
  pane.
- **Smaller fixes:** deselecting by mask said "selected"; the help overlay could be scrolled
  far past its end; PageDown in an empty folder left the cursor nowhere; members of an archive
  listed by an external tool showed as 0 B (their size is now left blank); a session path ending
  in a space came back without it.
- **Key files are tried even when an agent is running.** Once the agent's keys had failed, the
  keys in `~/.ssh` were never offered at all. They now go first, then the agent's; with a
  password the password goes first. An agent holding many keys no longer uses up the host's
  attempts and ends in "too many authentication failures".
- **ProxyJump works as ssh reads it:** a jump host's port is kept (it was dialled on 22),
  `a,b` chains through both, and `none` means no jump. Jump connections are closed with the
  session they carry, and the agent socket after each connect, instead of piling up.
- **A host that stops answering no longer hangs tyr.** The handshake gives up after 15 seconds,
  a session that stops answering keepalives is redialled, a listing or mkdir gives up after a
  minute, and `Esc` in the connection dialog cancels the connect.
- **A download stops when the local side fails** — a full disk, say — instead of waiting for
  ever on the host.
- **known_hosts follows ssh_config:** `UserKnownHostsFile`, `GlobalKnownHostsFile` and
  `HashKnownHosts yes` are honoured, and a known_hosts that cannot be written no longer stops a
  connect you just confirmed; tyr says the key was not saved.
- **More of ssh_config is read right:** options before the first `Host` line apply to every
  host, an `Include` inside a `Host` block only to that host, and `%h`, `%d`, `%u`, `%p`, `%r`
  are expanded in `IdentityFile` and the known_hosts files.
- **A key with a passphrase is named** when the password prompt appears, with a hint to load it
  into ssh-agent, instead of being skipped without a word.
- **Uploading over a file the host already has warns** like a local copy does, from the
  destination pane's listing.

### Added

- **The pack dialog asks for an archive password twice**, so a typing mistake does not lock
  the files away.

- **The editor is fit for quick code fixes:** syntax colours, undo and redo (`Ctrl+Z`/`Ctrl+Y`),
  find as you type (`Ctrl+F`, `F3`), selecting with `Shift` and the mouse, copy, cut and paste
  through the system clipboard, `Tab`/`Shift+Tab` to indent a selection, `Enter` keeping the
  indentation, and the cursor's line and column in the footer.

### Fixed

- **`v` on a named pipe no longer freezes tyr.** Opening it waited for a writer on the UI
  thread, for good; `/dev/zero` read until memory ran out. Pipes, sockets and devices are
  refused, files are read in the background, and the viewer holds the first 16 MB of a big one
  (the editor refuses it rather than truncate it on save).
- **Inspecting a hostile image is cheap.** A few hundred kilobytes of crafted metadata — one
  EXIF string referenced thousands of times, twenty zlib bombs in PNG text, XML nested twelve
  thousand deep, a C2PA assertion with millions of CBOR values, a profile spread over a thousand
  JPEG segments — cost gigabytes of memory and minutes to decode. Values are now shown up to
  4 KB with their real length, decompression is capped per image, and a report stops at about a
  megabyte.
- **"imagen" in a Spanish caption is no longer called an AI marker.** "imagen" and "grok" count
  only in the field naming the software. The location is taken from EXIF GPS only, not from a
  PNG text chunk that happens to be called `GPSPosition`.
- **Highlighting keeps accented letters.** Markdown, JSON and code showed `café` as `cafÃ©`; and
  a very long line took seconds to colour on every redraw.
- **`=` measures folders a few at a time and stops when you leave**, instead of walking up to two
  hundred trees at once and finishing them all regardless; pressing it twice no longer measures
  everything twice.
- Searching in the viewer no longer rebuilds the hex dump twice per keystroke; the address bar no
  longer reads the folder each time the cursor blinks; the capabilities overlay no longer searches
  `PATH` on every redraw.

### Fixed

- **Nothing is overwritten in place any more.** Every file tyr writes — a copy, a pack, an
  edited file, an archive member saved from the editor, the settings — is written beside its
  destination and moved into place once it is complete. A copy cancelled half-way, a pack that
  fails, a full disk while saving: each leaves the old file as it was, where before it left half
  a file or none. Overwriting a file with a copy no longer deletes the old one first.
- **Packing over an existing archive replaces it.** For `.zip` and `.7z` it added to the old
  archive, so files deleted since the last pack lived on in it.
- **Saving a file inside a plain `.tar` works on macOS and Windows.** It used `tar --delete`,
  which only GNU tar has; it now repacks, as compressed tars already did.
- **The editor notices a file changed underneath it.** Saving over a file that another program
  wrote to since it was opened now asks for a second `Ctrl+S`. A save also keeps a symlink a
  symlink and writes the file it points at.
- **Removing metadata from a read-only image works.** It always failed with "permission denied".
- On Linux, trashing a file never takes a name another program has just reserved in the trash,
  and never overwrites an existing `.trashinfo` record.

### Fixed

- **Batch rename no longer crashes on some names.** Find and replace with *ignore case* on
  a name holding a letter whose lower case is longer in bytes — `Ⱥ`, `İ` — closed tyr with a
  panic, on the keystroke that typed the pattern.
- **A failed batch rename no longer loses a file.** When a swap went wrong half-way, putting
  the rest back could move a file over the one that had just been renamed into its place. What
  already landed now stays, what is put back never replaces anything, and a file that cannot go
  back keeps its temporary name, which the error gives.
- **The preview shows a blocked chain as blocked.** Numbering `1.txt` and `2.txt` from 2 needs
  `2.txt → 3.txt`, which an unselected `3.txt` stops; `1.txt → 2.txt` showed as fine and then
  stopped the whole batch when applied.
- On Windows, names Windows refuses (`a:b`, `CON.txt`, a trailing dot) are held back in the
  preview instead of failing part-way through.
- The glob `[!abc]` means "not a, b or c", as in a shell, instead of also matching `!`.
- `Esc` stops a batch rename inside a big folder, rather than only between folders.

### Fixed

- **Synchronize no longer replaces a folder with a file.** A file `foo` on one side and a
  folder `foo/` on the other were listed as two one-sided rows, and running the sync deleted
  the folder and everything in it to copy the file across. A row that cannot be copied either
  way without losing something is now a conflict: it says what is in the way, is skipped, and
  cannot be turned into a copy. The same goes for `Foo.txt` against `foo.txt` on a volume that
  ignores case — one file there, so either copy would have overwritten the other, possibly the
  newer one — and for a copy that would land under a symlinked folder, outside the tree.
- **Comparing a folder with one inside it is refused**, as is comparing a folder with itself
  through a symlink. The sync would have copied the inner tree into itself.
- **Two different files with the same timestamp are skipped**, rather than copied left to right
  on no evidence.
- **Compare and find follow a symlinked starting folder.** On macOS, comparing anything under
  `/tmp` listed every file as one-sided, and a search from there found nothing.
- **Find keeps reading past a very long line.** A line over a megabyte — a minified bundle —
  ended the content search of that file, so a match further down was missed; its first
  megabyte is now searched and the rest skipped. A malformed pattern such as `[abc` is reported
  instead of matching nothing.
- Cancelling a byte-for-byte compare stops it straight away, not after the pair of files it
  was reading.

### Fixed

- **Copying or moving a file onto itself no longer deletes it.** A file reached through a
  symlinked folder — or under another case on a volume that ignores case — is the same file
  as the one in the folder, and answering "overwrite" removed it before copying from it. It
  is now refused, and so is copying a folder into itself, which used to copy itself a few
  hundred levels deep until the path got too long.
- **Unpacking asks before replacing.** Every extractor overwrites without asking, so an edited
  `notes.txt` was replaced by the archived one. An archive now unpacks beside what is there,
  and every name already taken is asked about, as a copy asks. Adding files into a `.tar` or
  `.zip` that already holds those names asks too, instead of replacing them.
- **Opening a file inside an archive opens that file.** The tools read a member's name as a
  pattern, so opening `a[1].txt` showed `a1.txt`, and saving it from the editor wrote the
  wrong contents back. Plain, gzip and bzip2 tar and zip archives are now read directly, by
  exact name, and a member over 64 MB is refused rather than read into memory. Browsing them
  shows real sizes instead of 0 B for every file.
- **Copies keep their modification times**, for files and folders, so a synchronize no longer
  finds everything it just copied newer on the other side and offers to copy it back. A
  read-only folder copies with its contents instead of failing on its first file.
- **Undoing a merge takes back only what arrived.** Moving `photos/` into a `photos/` that was
  already there recorded the whole folder, so undo moved the files that had been there before
  too. A copy into an existing folder is now undoable at all.
- **A named pipe no longer hangs a copy.** Pipes, sockets and devices are left out, and the
  job names them; a move to another drive that would leave one behind keeps the original.
- **A full disk while unpacking says so**, instead of being taken for a wrong password because
  a file in the archive had "encrypted" in its name — which asked for the password over and over.
- **A restore from the trash that fails on one item still restores the rest**, and `Ctrl+Z`
  keeps what is left instead of forgetting it.

### Security

- **On Windows, a file name can no longer change the command `x` runs.** cmd expands
  `%VAR%` even inside double quotes, so a file called `%COMSPEC%.txt` reached the command as
  `C:\Windows\system32\cmd.exe.txt` — and a variable holding a quote could break out of the
  quoting altogether. The placeholders now reach cmd through the environment
  (`"%TYR_ARG_1%"`), which cmd never re-expands, and the line is started with `cmd /S /C`
  exactly as written: until now Go escaped its quotes as `\"`, which cmd does not read, so
  any placeholder whose name had a space in it arrived mangled.
- **A name that starts with `-` is no longer read as an option.** `rm %f` on a file called
  `-rf` ran `rm -rf`; such a name is now passed as `./-rf`.

### Fixed

- `Esc` on a running command stops all of it. Only the shell was killed, so the rest of a
  pipeline (`make | tee log`) went on running and the command never finished.
- A command that will not stop printing (`yes`) is stopped once it has printed the 256 KB that
  is kept, instead of being read into memory until `Esc`.

### Security

- **Accepting an unknown host key trusts that key, not the host.** The retry after
  `a` accepted whatever key it was offered, so an interceptor that let the first attempt
  through to the real server — the fingerprint you checked was genuine — could answer the
  retry with its own key and receive the password. The retry, and every reconnect after it,
  now trusts exactly the fingerprint that was shown. An unknown jump host is asked about
  under its own name, instead of under the target's in a prompt that came back forever.
- **A download only writes what was asked for.** The archive a host sends back is checked
  entry by entry, and anything outside the requested names stops the transfer before it is
  written: a hostile host could otherwise answer a download into `~` with
  `.ssh/authorized_keys` or `.zshrc`. Downloads and uploads no longer restore the
  archive's owner, and uploads from macOS no longer carry extended attributes (where a file
  was downloaded from, among them) or `._` files to the host.
- **Remote names are taken as names.** A name starting with `~` was treated as relative to
  the home directory, so deleting `~$report.docx` in `/srv/x` ran `rm -rf` on that name in
  `$HOME`. A remote listing could also show one name as another — a leading space was lost
  (` data` listed as `data`) and a symlink called `data -> old` read as `data` — so an
  operation on the entry acted on a different file. Listings now carry the directory's
  real names and every row is matched against them. A name starting with `@` no longer
  makes macOS's `tar` include another archive in a transfer.
- **The password is no longer sent to every prompt.** A host asking for a second factor
  over keyboard-interactive got the password as the answer to "Verification code:" too.
  It is now given once, to a single hidden prompt, and anything else is refused with the
  question the host asked.
- **A directory name can no longer rewrite the config.** The config is one setting per
  line, and a line break in the path saved for the session turned the rest of it into a
  setting of its own — enough for a downloaded archive's folder name to repoint a saved
  connection. Such a path is not remembered, and any other value with a line break is
  refused instead of written.

### Fixed

- Typing `~` or `~/dev` in a remote pane's address bar goes to the home directory, as it
  always meant to; it used to fail with "no such file or directory".
- Saving or deleting a connection no longer wipes others whose name starts with its name
  and a dot: saving `web` deleted `web.staging`.
- Entering a remote directory whose name ends in a space no longer lands somewhere else.

### Security

- **Names and file contents can no longer send commands to the terminal.** A file name, an
  archive member, a remote listing, a line in the viewer or the find results, and an error
  message are all text from somewhere else, and any of them could carry an escape sequence —
  enough for a file name to write the clipboard (OSC 52), retitle the window or repaint the
  screen just by being listed. Such text is now shown escaped (`\u001b`, `\n`) wherever it is
  drawn, and every frame is filtered on its way out so that only tyr's own colours reach the
  terminal, whatever path the text came by.
  - An ANSI-coloured log opened in the viewer shows its escape codes instead of its colours.
  - An error now stays on the status bar's one line: a multi-line message is joined with `·`
    and cut to fit, instead of pushing the panes up off the screen.
  - A find hit in a long line keeps the part around the match, up to 256 bytes, rather than
    the whole line.

## [0.4.0] - 2026-10-16

### Added

- **Audio conversion** (`C`): converts the selected files into the other pane as `ogg`
  (Vorbis), `opus`, `mp3`, `m4a` (AAC), `flac` or `wav`, one new file per source with the
  originals left alone. A format and quality picker comes first. `ffmpeg` does the work, and the
  dialog names the encoder your build will use; when `.ogg` has to fall back to ffmpeg's
  experimental built-in Vorbis encoder, the dialog warns in red. Video files give up their first
  soundtrack, and a selected folder converts whole, keeping its shape and skipping playlists
  and dotfiles. Tags carry over; cover art does not.
  - Two progress bars, one for the batch and one for the file in hand with its position, timed
    against the length `ffprobe` reports.
  - Each file is written under a temporary name and renamed into place when ffmpeg finishes, so
    a failure or `Esc` never leaves half a file. A taken name asks like a copy does, and
    `Ctrl+Z` removes what the conversion wrote.
  - The capabilities overlay lists it under `ffmpeg`.
- **Clean image copies**, on the same `C`: a JPEG, PNG, GIF or WebP converts into the other
  pane as a copy with its metadata removed — EXIF and GPS, XMP, IPTC, ICC, comments, C2PA,
  thumbnails, trailing data. The image data is copied byte for byte, never re-encoded, and no
  external tool is needed. The dialog keeps the EXIF orientation by default, so portrait phone
  photos stay upright, and can keep the ICC profile too. A selection or folder holding audio and
  images converts both, and images still go ahead when ffmpeg is missing. When both panes show
  the same directory the images are stripped in place, behind a confirm prompt that says the
  originals are overwritten and cannot be restored by `Ctrl+Z`.
- **Image metadata in the viewer**: `v` on a JPEG, PNG, GIF or WebP shows everything it
  carries, decoded, instead of a hex dump — the GPS position first, then provenance and
  AI-generation markers (C2PA credentials and their signer, declared digital source type,
  soft-binding watermarks, Stable Diffusion / ComfyUI / InvokeAI traces, generator names), then
  every item with its fields. `x` switches to hex and back. Escape sequences inside metadata are
  shown escaped, never sent to the terminal.

### Changed

- **The capabilities overlay is reached from help**: `?`, then `Tab` (and `Tab` back). It no
  longer has a key of its own by default, since `C` now converts; the key editor can give it
  one again under `capabilities`.

### Security

- Bumped `golang.org/x/crypto` to v0.56.0 for GO-2026-6354 and GO-2026-6355: a peer could
  deadlock an ssh channel and hang a remote connection.

## [0.3.0] - 2026-08-28

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
