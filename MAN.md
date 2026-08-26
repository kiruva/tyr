# tyr manual

Everything tyr does, key by key. For what tyr is and how to install it, see
[README.md](README.md).

## Requirements

Browsing, copying, and editing need nothing but the binary. Archive actions call the system
tool for the format you touch (`tar` plus `gzip`/`bzip2`/`xz`/`zstd`, `zip`/`unzip`, `7z`,
`unrar`), and ssh transfers need `tar` and a POSIX shell on the far side. 7-Zip is the one
worth installing: it gives AES-256 encryption for `.7z` and `.zip`, and it also stands in for
`zip`/`unzip` where those are missing. Whichever name it is packaged under, `7z` (p7zip),
`7zz` (the official build) or `7za`, tyr finds it and runs that one. Press `C` in the app to
see which of those this machine actually has (see [Capabilities](#capabilities)). A fresh
Windows box has `tar` and little else; [On Windows](#on-windows) says what that costs.

## On Windows

Everything in this manual applies on Windows. What differs is where things live and which
shell runs a command:

| What | On Windows |
| --- | --- |
| Config file | `%AppData%\tyr\config`, themes in `%AppData%\tyr\themes`. `XDG_CONFIG_HOME` still wins where it is set. |
| Trash | `%LocalAppData%\tyr\Trash`, with the same origin records, so `Ctrl+Z` restores. It is tyr's own trash, **not** the Recycle Bin. |
| `!` | Opens `%COMSPEC%` (`cmd.exe` on a stock install), or `$SHELL` where that is set. |
| `x` | Runs the line through `cmd /C`, and quotes the placeholders by cmd's rules rather than a POSIX shell's. |
| `i` | No owner row and no hard-link count: a Windows file has an ACL, not a uid and gid. The permissions field still applies, but `chmod` there sets the read-only attribute and nothing more. |
| `.` | Toggles dotfiles, as everywhere else. The Windows *hidden attribute* is not consulted, so a hidden file without a leading dot is always listed. |

Paths work the way the platform does: drive letters (`C:\Users\kim`), backslashes, and UNC
shares (`\\server\share`) are all valid in the address bar, and a drive letter is never
mistaken for an ssh target even though scp syntax puts a colon in the same place. Remote paths
stay POSIX, because the far side is.

Archive tools are where a fresh Windows box is thinnest. Windows 10 and 11 ship `tar.exe`, so
the tar formats extract out of the box, but `gzip`, `bzip2`, `xz`, `zstd`, `zip` and `unzip`
are usually not there. Installing 7-Zip covers `.7z`, `.zip`, and AES-256 passwords for both.
Press `C` to see what this machine actually has.

Run it in Windows Terminal, or any console with VT support. The legacy `conhost` window draws
the box characters and colours poorly.

## Run it

```sh
tyr                       # or: go run .
tyr --theme nord          # start with a theme
tyr --themes              # list the built-in themes
tyr --new-theme rose      # write a theme file to edit, then exit
tyr --cd-file /tmp/where  # write the directory it exits in, for a shell wrapper
tyr --version
tyr --help
```

See [Following tyr out](#following-tyr-out) for the shell function `--cd-file` is for.

## Keys

| Key                 | Action                          |
| ------------------- | ------------------------------- |
| `j` / `↓`           | Move cursor down                |
| `k` / `↑`           | Move cursor up                  |
| `Ctrl+D` / `Ctrl+U` | Page down / up                  |
| `PgDn` / `PgUp`     | Page down / up                  |
| `g` / `Home`        | Jump to top                     |
| `G` / `End`         | Jump to bottom                  |
| `Enter` / `l` / `→` | Open directory or archive       |
| `h` / `←` / `⌫`     | Go to parent dir                |
| `Ctrl+L` / `:`      | Edit address bar (jump to path) |
| `Tab`               | Switch active pane              |
| `Ctrl+R` / `R`      | Re-read the active pane         |
| `/`                 | Filter the pane (`Esc` clears)  |
| `Ctrl+F` / `F`      | Find files below this directory |
| `Space`             | Select / deselect (sizes a dir) |
| `Ctrl+A`            | Select everything visible       |
| `*`                 | Invert the selection            |
| `+` / `-`           | Select / deselect by mask       |
| `=`                 | Measure every directory shown   |
| `s`                 | Cycle sort (name → size → time) |
| `.`                 | Toggle hidden files             |
| `n`                 | New file in active pane         |
| `N` / `F7`          | New folder in active pane       |
| `F5` / `c`          | Copy selection → other pane     |
| `F6` / `m`          | Move selection → other pane     |
| `F8` / `Del` / `d`  | Delete selection (to the trash) |
| `D` / `Shift+F8`    | Delete permanently              |
| `r` / `F2`          | Rename the highlighted entry    |
| `M`                 | Multi-rename tool (batch)       |
| `Ctrl+Z`            | Undo the last operation         |
| `F9` / `Y`          | Compare & synchronize the panes |
| `i`                 | Properties & permissions        |
| `b` / `B`           | Bookmarks / bookmark this dir   |
| `!` / `x`           | Shell here / run a command      |
| `p`                 | Pack dialog: format/level/password |
| `u`                 | Unpack archive → other pane     |
| `U`                 | Unpack archive in place         |
| `v`                 | View file (read-only)           |
| `e`                 | Edit file (nano-style)          |
| `S` / `Ctrl+S`      | ssh connections                 |
| `t`                 | Theme picker                    |
| `K`                 | Edit the keybindings            |
| `?`                 | Show all keybindings            |
| `C`                 | Show capabilities on this box   |
| `y` / `n`           | Confirm / cancel a prompt       |
| `q` / `Ctrl+C`      | Quit                            |

Press `?` any time for the full keybinding overlay, grouped by what each key does, and `C`
for the capabilities overlay (see [Capabilities](#capabilities)) — the two swap into each
other, and both scroll when the terminal is too short to hold them. Directories always sort
before files; `s` orders the entries within each group.

### Rebinding them

None of the above is fixed. `K` opens the keymap:

```
 keys · 47 actions                                    saved to the config file
───────────────────────────────────────────────────────────────────────────────
▸ Navigate   up                       ↑ k
             down                     ↓ j
  Select     select / size dir        space
  Look       filter (esc clears)      /
 enter rebind · a add a key · d default · D all defaults              esc close
```

`Enter` waits for the key you want and binds it; `a` adds a second key to the same action;
`d` puts one action back to its default and `D` puts every one back. A key that already
belongs to something else is refused and told whose it is, because two actions on one key is
not a preference. `Ctrl+C` and `Esc` cannot be rebound — they are how you get out of things.

Every change is written to the config file as it is made, one line per rebound action, which
is also how to do it by hand:

```ini
key.copy = f5,c
key.sort = z
key.select = space      # the space bar and the comma are written as words
```

The action names are the ones the editor lists; a name tyr does not know is reported in the
status bar at startup rather than silently doing nothing. The help overlay is generated from
whatever the keys currently are, so it never describes a binding you have changed.

The keys *inside* a tool — the pager's `w`, the sync list's arrows, `y`/`n` on a prompt — are
local to that tool and are not rebindable.

## The mouse

Clicking is not how a file manager is driven, but it is how one is pointed at:

| Action           | What it does                                          |
| ---------------- | ----------------------------------------------------- |
| left click       | focus that pane and put the cursor on that row         |
| double click     | open the entry under the pointer                       |
| right click      | mark the entry, without walking the cursor on          |
| click the path   | open that pane's address bar                           |
| wheel            | scroll the pane, list, or file under the pointer       |

With a dialog open the pointer is aiming at the dialog, so clicks do not reach the panes
behind it; the wheel still moves through whichever list is in front. Everything the mouse does
has a key that does the same, so nothing depends on having one.

```ini
mouse = off      # leave the pointer to the terminal, for selecting and pasting
```

## Address bar

Each pane's top line is its address bar. It follows the cursor as you walk the tree, and
`Ctrl+L` (or `:`) turns it into an input for jumping straight to a path — `Enter` goes,
`Esc` cancels, `Tab` completes, `↑`/`↓` cycle completions. Paths may be absolute, relative
to the current directory, `~`-rooted, or contain `$VARS`. Typing the path of a browsable
archive opens it as a virtual tree, and an `ssh://` or `host:/path` target starts the ssh
connection flow for that pane (see [Over ssh](#over-ssh)); anything that isn't a directory
leaves the bar open with the reason in the status line.

## Filtering

`/` narrows the active pane as you type: the listing behind the prompt shrinks on every
keystroke, so a pattern is judged by what it leaves rather than by what it says. `Enter` keeps
the narrowed pane and hands the keys back to navigation, `Esc` puts back whatever was there
before — and with nothing else open, `Esc` clears the filter entirely.

A pattern with a wildcard in it (`*`, `?`, `[…]`) is matched as a glob against the whole name;
anything else matches as a substring. Both ignore case.

```
/ *.go        →  every Go file
/ test        →  anything with "test" in the name
```

The filter is part of the view, not of the directory: it survives a refresh, and walking
anywhere drops it. Everything that acts on "what is on screen" — `Ctrl+A`, `*`, `+`, `-` —
respects it, which is the point of having both.

## Finding

`Ctrl+F` searches the active pane's directory and everything under it:

```
╭──────────────────────────────────────────────────────────╮
│   Find                                                   │
│                                                          │
│   in /home/kim/src/tyr                                   │
│                                                          │
│   name      *.go                                         │
│   contains  TODO                                         │
│                                                          │
│     [×] match case                                       │
│     [ ] hidden files                                     │
│                                                          │
│    enter  search     tab  field     esc  close           │
╰──────────────────────────────────────────────────────────╯
```

Either field on its own is a search; both together mean *and*. **name** takes the same globs
and substrings the filter does. **contains** looks inside the files — binary files and
anything over 32 MB are matched by name only, and the hit reports the line it landed on.

The walk runs off the UI thread and `Esc` cancels it. Results list the path relative to where
the search started, with the size or the matching line beside it: `Enter` takes the pane to
that file with the cursor already on it, `/` goes back to the query to narrow it, `Esc` closes.
Long searches stop at 500 hits and say so rather than pretending that was all of them.

Find walks this machine's filesystem — it is not available on a remote pane, and an archive is
already listed in full by the pane itself.

## Selecting

`Space` marks one entry and moves down. Beyond that:

| Key       | What it marks                                      |
| --------- | -------------------------------------------------- |
| `Ctrl+A`  | everything visible                                  |
| `*`       | the inverse of what is marked now                   |
| `+`       | the entries matching a mask                         |
| `-`       | unmarks the entries matching a mask                 |

`+` and `-` open a one-field prompt. A mask is one or more patterns separated by `;`, each a
glob when it has a wildcard and a substring otherwise:

```
*.go;*.md     →  Go and Markdown files
draft         →  anything with "draft" in the name
```

All four act on what the pane is showing, so under a filter they reach only what is left.

## Sizes

A directory's own size says nothing about what is in it, so the size column stays blank until
something measures one. `Space` on a directory measures it while it marks it; `=` measures
every directory in the pane. Both walk the tree off the UI thread and fill the column in as
the totals land, and a pane sorted by size reorders as they do — with the cursor following the
entry it was on, not the row.

Totals are remembered until the pane moves somewhere else, so a refresh keeps them. Measuring
is local: a remote pane says so rather than walking the far side.

## Creating

`n` names a new file and `N` (or `F7`) a new folder, in the active pane's current directory —
local or remote. The name may contain separators, so `src/main.go` creates the missing
directories on the way. Absolute paths are refused (use the address bar to move there), and so
is a name that already exists: creating never overwrites.

## Renaming

Two tools, one engine. `F2` fixes a single name; `M` reshapes a whole selection from a
pattern, with a live preview of every name before anything moves. `Ctrl+Z` puts either of
them back.

### One name at a time

`r` (or `F2`) renames the highlighted entry. The prompt starts prefilled, so it is an edit
rather than a retype:

```
╭──────────────────────────────────────────────────────────╮
│   Rename                                                 │
│                                                          │
│   quarterly-report-draft.md                              │
│   quarterly-report-final.md                              │
│                                                          │
│    enter  rename     esc  cancel     M for a batch       │
╰──────────────────────────────────────────────────────────╯
```

The name stays where it is: separators, absolute paths and `..` are refused — use `F6`/`m` to
move something. An existing name is never overwritten, and changing only the case of a name
works even on a case-insensitive filesystem.

### The multi-rename tool

`M` opens the batch tool on the selection, or on the highlighted entry when nothing is marked.
It is full-screen because the preview is the point: nothing happens until you press `enter`
and confirm.

```
 multi-rename · 4 items · 3 to rename · 1 blocked                          ~/pics
  find     IMG_(\d+)                        regex · ignore case
  replace  holiday-$1
  name     [N]              ext [E]      counter 1     step 1
  case as-is · trim · recursive · rename dirs
─────────────────────────────────────────────────────────────────────────────────
  IMG_0021.jpg      → holiday-0021.jpg
  IMG_0022.jpg      → holiday-0022.jpg
  IMG_0023.jpg      → holiday-0023.jpg ✗ exists
  sub/IMG_0099.jpg  → sub/holiday-0099.jpg
 tab field · ↑/↓ scroll · enter apply · esc cancel
 ctrl+o mode · alt+i ignore case · ctrl+t case · ctrl+p trim · ctrl+r recursive · …
```

The header counts what the form would do; the highlighted switches on the line under it are
on. Rows that cannot be applied are marked `✗` with the reason and are simply left alone —
the rest of the batch still runs.

| Key                 | Does                                              |
| ------------------- | ------------------------------------------------- |
| `Tab` / `Shift+Tab` | Next / previous field                             |
| `↑` / `↓`           | Scroll the preview                                |
| `PgUp` / `PgDn`     | Scroll the preview a page                         |
| `Ctrl+O`            | Cycle how `find` is read: regex → literal → glob  |
| `Alt+I`             | Match without regard to case                      |
| `Ctrl+T`            | Cycle case conversion: as-is → lower → UPPER → Title |
| `Ctrl+P`            | Trim and collapse whitespace                      |
| `Ctrl+R`            | Recursion on/off                                  |
| `Ctrl+Y`            | Rename directory names too                        |
| `Enter`             | Apply (asks to confirm first)                     |
| `Esc`               | Close, changing nothing                           |

While `name` or `ext` has focus, the bottom line becomes the token legend, so the masks are
documented where you are typing them.

#### A first run

1. Mark what to rename with `Space`, or just leave the cursor on one entry.
2. Press `M`. The preview lists every candidate; on an untouched form each one reads
   `(unchanged)`, because the default masks reproduce the name it already has.
3. Type a pattern into `find`, `Tab` to `replace`, and type the replacement. The preview
   follows every keystroke.
4. Read the header: how many the form renames, and how many it cannot. Scroll the list with
   `↑`/`↓` if it is longer than the screen.
5. `Enter`, then `y`. If it turns out wrong, `Ctrl+Z` puts it back.

Nothing before step 5 touches the disk, so a pattern is free to experiment with.

#### How a new name is built

Four fields, applied in this order. Knowing the order is most of knowing the tool:

1. **`find` decides who takes part.** A name it does not match is left exactly as it is, masks
   and all — so `find` doubles as a filter over the whole form. An empty `find` matches
   everything. It is matched against the name as it stands now, before any mask has run.
2. **`name` and `ext` are masks.** They build a name out of literal text and `[tokens]`.
   Their defaults, `[N]` and `[E]`, reproduce the current name — so a form you have not
   touched renames nothing.
3. **The two are joined** into `name.ext`, or just `name` when `ext` is empty.
4. **`replace` substitutes** into that whole assembled name, wherever `find` matched. It takes
   the same `[tokens]` the masks do, alongside its `$1` backreferences.
5. **Case conversion and trimming** run last, on the name without its extension.

Following `IMG_0021.JPG` through a form with `find` = `IMG_(\d+)`, `replace` = `holiday-$1`
and case set to `lower`:

```
  IMG_0021.JPG
    masks       [N] → IMG_0021        [E] → JPG
    joined      IMG_0021.JPG
    substitute  holiday-0021.JPG      ← the pattern never matched ".JPG", so it survives
    case        holiday-0021.JPG      ← lower applies to the stem, which is already lower
```

Two consequences worth knowing before you type a pattern:

- **`find` sees the extension.** It is matching `IMG_0021.JPG`, not `IMG_0021`. That is what
  lets a pattern rewrite part of a name and leave the rest — including the extension —
  untouched. It is also why a greedy pattern like `(.+) - (.+)` swallows `.mp3` into `$2`;
  anchor it (`^(.+) - (.+)\.mp3$`) when the extension matters.
- **Case and trim do not.** `UPPER` will not shout your `.jpg` into `.JPG`. To change an
  extension, put the new one in the `ext` field.

A directory has no extension to protect: its whole name is `[N]`, and `ext` is ignored.

#### How `find` is read

`Ctrl+O` cycles the three modes. `Alt+I` makes any of them case-insensitive.

| Mode      | The pattern is                                | The replacement is        |
| --------- | --------------------------------------------- | ------------------------- |
| `regex`   | a Go regular expression, matched anywhere      | text with `$1` backrefs   |
| `literal` | plain text — `.` and `*` mean themselves       | plain text                |
| `glob`    | `*`, `?` and `[abc]`, matched against the whole name | text with `$1` per wildcard |

An empty `find` matches every name, which is what you want when the masks are doing the work.
A regex that does not compile is reported under the form and cannot be applied.

#### Tokens

Tokens work in three fields — `name`, `ext` and `replace` — and mean the same thing in each.
They are resolved per file, against the name as it is now:

| Token    | Is                                       |
| -------- | ---------------------------------------- |
| `[N]`    | the name without its extension           |
| `[N3]`   | its 3rd character                        |
| `[N3-7]` | characters 3 to 7 (1-based, inclusive)   |
| `[N3-]`  | from the 3rd character to the end        |
| `[E]`    | the extension, without the dot           |
| `[E1-3]` | the extension, sliced the same way       |
| `[C]`    | the counter                              |
| `[C3]`   | the counter, zero-padded to 3 digits     |
| `[P]`    | the name of the directory the file is in |
| `[d]`    | the file's own date, as `2026-08-24`     |
| `[t]`    | its time, as `09-41-12`                  |

A range that runs past the end of a short name yields what is there rather than failing. An
unknown token is left as typed, so `[Z]` stays `[Z]` and a name may legitimately contain a
bracket.

In `replace`, tokens sit alongside backreferences — `holiday_[d]_$1` is a date from the file
and a group from the pattern in one replacement. `find` itself takes no tokens: it is a
pattern, not a name.

One Go regexp quirk to know when you mix the two: a `$1` that runs straight into letters,
digits or an underscore is read as one long group name, so `$1_[d]` looks for a group called
`1_2026`. Brace it — `${1}_[d]` — whenever something wordlike follows.

**counter** and **step** drive `[C]`: it starts at `counter` and advances by `step` for each
name the pattern matches — a file `find` skipped does not burn a number. `step` may be
negative to count down.

#### Recipes

Each of these is a form to type and what it does to the files under it. Every one is pinned by
a test, so they work as written. Fields not shown are left at their defaults.

**Numbering.** The counter is the reason the tool exists for photo dumps:

```
Number a batch, keeping each extension        Keep the name, add a number in front
  name  holiday-[C3]                            name  [C2]-[N]

  DSC_4821.JPG → holiday-001.JPG                intro.md → 01-intro.md
  DSC_4830.JPG → holiday-002.JPG                setup.md → 02-setup.md
  DSC_4901.JPG → holiday-003.JPG

Start somewhere else, count in tens           Number only what the pattern matched
  name  [C]   counter  10   step  10            find  IMG        name  photo-[C]

  a.txt → 10.txt                                IMG_11.jpg → photo-1.jpg
  b.txt → 20.txt                                scan.jpg   → scan.jpg     ← skipped
  c.txt → 30.txt                                IMG_12.jpg → photo-2.jpg  ← still 2
```

**Substituting.** `Ctrl+O` picks how the pattern is read; the mode is named in each stanza:

```
Strip a leading track number (regex)          Swap the halves of a name (regex)
  find     ^\d+ -_       ← "_" is a space        find     ^(.+) - (.+)\.mp3$
  replace  (empty)                              replace  $2 - $1.mp3

  01 - Intro.mp3 → Intro.mp3                    Miles Davis - So What.mp3
  02 - Verse.mp3 → Verse.mp3                      → So What - Miles Davis.mp3

A version number, dots and all (literal)      Rewrite a prefix, keep the rest (glob)
  find     v1.2                                 find     draft-*
  replace  v2.0                                 replace  2026-$1

  app v1.2.zip → app v2.0.zip                   draft-notes.txt → 2026-notes.txt
                                                final-notes.txt → final-notes.txt
                                                     └── glob matches the whole name or not
                                                         at all, so this one is untouched

Underscores to dashes (literal)               Match whichever case it is (literal, alt+i)
  find     _                                    find     img
  replace  -                                    replace  photo

  my_long_name.txt → my-long-name.txt           IMG_1.jpg → photo_1.jpg
                                                img_2.jpg → photo_2.jpg
```

**Stamping on a date or a number.** Tokens in `replace` write the same values the masks do,
but only where the pattern matched — so the rest of the name is left exactly as it was:

```
Date the whole batch (masks)                  Date only what matches (regex)
  name  [N]_[d]                                 find     ^(report[^.]*)
                                                replace  ${1}_[d]
  notes.md → notes_2026-08-24.md
  todo.md  → todo_2026-08-24.md                 report-q3.pdf → report-q3_2026-08-24.pdf
                                                notes.pdf     → notes.pdf

A token and a backreference together (regex)  Version-number what was found (literal)
  find     IMG_(\d+)                            find     draft
  replace  holiday_[d]_$1                       replace  v[C3]

  IMG_0021.jpg                                  draft-a.txt → v001-a.txt
    → holiday_2026-08-24_0021.jpg                draft-b.txt → v002-b.txt
```

**Extensions.** The `ext` field owns the extension; `find` and the case keys leave it alone:

```
Change it                Normalise a shouting one         Drop it
  ext  md                  ext  jpg                         ext  (empty)

  notes.txt → notes.md     DSC_1.JPG → DSC_1.jpg            README.md → README
  todo.txt  → todo.md
```

**Slicing and tidying.** `[N]` can be cut down, and the case keys clean up what is left:

```
Shorten to the first 8 characters             Cut off a fixed-width prefix
  name  [N1-8]                                  name  [N12-]

  a-very-long-filename.log                      2026-08-24 meeting.md → meeting.md
    → a-very-l.log                                         └── 11 characters, so start at 12

Fix the shouting and the double spaces        Stamp on the file's own date
  ctrl+t → Title      ctrl+p → trim             name  [d] [N]

  "  my   HOLIDAY  photo .jpg"                  report.pdf → 2026-08-24 report.pdf
    → My Holiday Photo.jpg
```

**Flattening.** `[P]` is the folder a file came from, which is what you want when a recursive
batch is about to put files from several folders side by side:

```
  name  [P]-[N]

  holiday/1.jpg → holiday/holiday-1.jpg
  work/1.jpg    → work/work-1.jpg
```

Renaming does not move anything, so those files stay in their folders — `[P]` only makes the
names safe to move together afterwards with `F6`.

#### Choosing what to rename

Candidates come from the selection (`Space`), or from the highlighted entry when nothing is
marked. `..` is never one.

- **`Ctrl+R` — recursion**, on by default. A selected directory contributes everything inside
  it, at any depth, and each preview row is prefixed with where it lives. With recursion off,
  a selected directory contributes only itself.
- **`Ctrl+Y` — rename directories**, off by default. Until you turn it on, a directory is
  something to look inside, not something to rename, so a recursive pattern cannot reshape the
  tree by accident. With it on, children are renamed before their parent, so both can change in
  one batch.
- **Dotfiles** come along only when the pane is showing them (`.`).
- A sweep stops at **20 000 entries** and says so in the header, rather than building a preview
  nobody can read.

#### When it refuses

A row marked `✗` is skipped and counted in the confirm prompt:

| Marked                | Means                                                          |
| --------------------- | -------------------------------------------------------------- |
| `✗ exists`            | something is already using that name, and the batch does not move it away |
| `✗ same name as …`    | two candidates in one directory want the same name              |
| `✗ empty name`        | the masks produced nothing                                      |
| `✗ name contains a separator` | the result is a path, and rename does not move things    |
| `✗ reserved name`     | `.` or `..`                                                     |

Nothing is ever renamed over an existing file — not even by a batch that raced with something
else, which stops with an error rather than overwriting. What looks like it needs overwriting
usually does not: a swap (`a → b`, `b → a`), a chain (`a → b`, `b → c`) and a change of case
alone are all staged through temporary names, and simply work.

Rename is local. An ssh pane or an archive is refused with the reason in the status line —
copy the files across, or unpack, first.

### Undo

`Ctrl+Z` puts the last rename back — the batch or a single `F2`, whichever came last — after
the usual confirmation:

```
╭─────────────────────────────────────────────────╮
│   Undo rename                                   │
│                                                 │
│   put 2 folders and 2 files back                │
│   in ~/projects                                 │
│                                                 │
│    y  confirm     n  cancel                     │
╰─────────────────────────────────────────────────╯
```

Press it again to walk further back. The history is 20 operations deep — renames among them —
kept in memory for the session and never written to disk; there is no redo, and cancelling the
prompt leaves the history alone.

What is recorded is what actually happened rather than what was planned, so a batch that
stopped part-way is still undoable, and a recursive batch that renamed directories as well as
their contents is taken apart in the opposite order it went together. An entry whose name has
moved on since — renamed by hand, or its old name taken again — is skipped and counted in the
prompt; the rest still goes back. When nothing in a batch can be put back, tyr says so and
drops it from the history rather than leaving it in the way of the ones underneath.

A rename shares the history with everything else that can be put back — see
[Undo](#undo-1) for what a copy, a move or a delete does there.

## Operations

Copy (`F5`/`c`), move (`F6`/`m`), and delete (`F8`/`Del`/`d`) act on the entries marked with
`Space`, or on the highlighted entry when nothing is marked. Copy, move, pack, and unpack all
go from the **active** pane to the **other** pane, so direction is whatever `Tab` says it is.

Every operation is recursive, asks for confirmation first, and runs off the UI thread,
streaming progress into a bar while the interface stays responsive. Both panes refresh when it
finishes.

### When a name is taken

A copy or a move that lands on a name already in use stops and asks, with both files side by
side and the newer one marked:

```
╭──────────────────────────────────────────────────────────╮
│   Already there                                          │
│                                                          │
│   in /home/kim/backup                                    │
│   report.pdf                                             │
│                                                          │
│   incoming       1.2MB     2026-08-24 15:04  newer       │
│   already there  980KB     2026-08-02 09:12              │
│                                                          │
│    o  overwrite               s  skip                    │
│    b  keep both               u  overwrite if newer      │
│                                                          │
│   capitals answer the rest · esc cancels                 │
╰──────────────────────────────────────────────────────────╯
```

`o` replaces it, `s` leaves it, `b` writes the incoming file as `report (2).pdf`, and `u`
decides on the timestamps. The capital of each key — `O`, `S`, `B`, `U` — answers the same way
for every remaining collision, so a hundred-file copy takes one keypress. `Esc` cancels the
job; whatever it had already copied stays, and is still in the undo history.

Two directories of the same name **merge** rather than colliding — the question is asked file
by file, inside them. A job that already knows the answer never asks: a synchronize overwrites
by design, and says so on its own prompt.

### Deleting, and getting it back

`F8` (or `Del`, or `d`) moves the selection to the desktop trash — `$XDG_DATA_HOME/Trash` on
Linux, with the `.trashinfo` record that lets any file manager restore it, `~/.Trash` on
macOS, and `%LocalAppData%\tyr\Trash` on Windows (tyr's own, not the Recycle Bin: see
[On Windows](#on-windows)). `Ctrl+Z` puts it straight back. `D` (or `Shift+F8`) skips the trash and unlinks, which
is the one thing here that cannot be undone; the prompt says which of the two you are about to
do. Trashing needs the file to be on the same filesystem as the trash — when it is not, tyr
says so rather than quietly copying gigabytes across a disk boundary.

Put `delete = remove` in the config file to make `F8` the permanent one.

### Undo

`Ctrl+Z` reverses the last operation, and keeps twenty of them:

| What happened      | What Ctrl+Z does                                     |
| ------------------ | ----------------------------------------------------- |
| rename (`F2`, `M`) | renames back                                           |
| move               | moves the files back where they came from              |
| copy               | removes the copies it made — and only those            |
| delete to trash    | restores from the trash                                |

A copy that overwrote something records nothing: undoing it would mean deleting a file that
was already there. The history lives in memory for the session only, and nothing over ssh or
inside an archive goes into it.

## Comparing and synchronizing

`F9` (or `Y`) compares the two panes, recursively, and lists what does not match:

```
 compare · 3 differ · 2 only left · 1 only right · 40 same          5 → · 1 ←
 /home/kim/src/tyr                              /media/backup/tyr
────────────────────────────────────────────────────────────────────────────
 README.md              12KB 2026-08-24 15:04  →   9KB 2026-08-02 09:12
 internal/app/view.go    8KB 2026-08-24 14:51  →    —
 notes/old.md              —                   ←   2KB 2026-08-01 11:20
```

Every row arrives with a direction already proposed: a file only one side has gets copied
across, and where both have it the newer one wins. `→` and `←` change a row's direction, `s`
skips it, and each of them steps down so a column of decisions takes one keypress each. `a`
puts every row back to what was proposed.

`e` shows the files that match as well, `.` includes dotfiles, and `c` compares same-size files
byte for byte rather than trusting their timestamps — each re-runs the walk. `Enter` goes to
the usual confirm prompt, which states how many copies go each way before anything is
overwritten. Files are compared by size and modification time, with a two-second tolerance,
because filesystems disagree about the fractions.

Comparing is local: transfer files across first, and unpack an archive before comparing it.

## Properties & permissions

`i` opens what an entry is:

```
╭──────────────────────────────────────────────────────────╮
│   deploy.sh                                              │
│   /home/kim/src/tyr/deploy.sh                            │
│                                                          │
│   type        file                                       │
│   size        4.2KB  (4302 bytes)                        │
│   modified    2026-08-24 15:04:11                        │
│   owner       kim:staff                                  │
│   mode        -rw-r--r--                                 │
│                                                          │
│   permissions 0644                                       │
│                                                          │
│    enter  apply     esc  close                           │
╰──────────────────────────────────────────────────────────╯
```

The permissions field takes octal — `644`, `755`, `0600` — and `Enter` applies it. On a
directory, `Tab` reaches a switch that applies the change to everything inside, which runs as a
normal job with a progress bar. A symlink is left alone: `chmod` would follow it and change
something you did not point at.

## Bookmarks

`B` saves the directory the active pane is in, suggesting its own name as the label; `b` lists
what is saved, `Enter` goes there, `d` removes one. They are one line each in the config file:

```ini
bookmark.src = /home/kim/src
bookmark.dl = /home/kim/downloads
```

## The shell

`!` hands the terminal to `$SHELL`, started in the active pane's directory. tyr comes back when
the shell exits, and refreshes the pane — you were in a shell, so something in there has
probably changed.

`x` runs one command line in that directory and shows what it printed in the pager. The pane's
state goes in through placeholders, each quoted so a filename cannot be read as shell syntax:

| Placeholder | What it becomes                       |
| ----------- | ------------------------------------- |
| `%f`        | the name under the cursor             |
| `%F`        | its full path                         |
| `%s`        | every marked entry, space separated   |
| `%d`        | the active pane's directory           |
| `%D`        | the other pane's directory            |
| `%%`        | a literal `%`                         |

`Esc` cancels a command that is taking too long.

### Following tyr out

A program cannot change its parent shell's directory, so tyr writes where it ended up and lets
the shell read it:

```sh
tyr() {
  local dir
  dir=$(mktemp -t tyr-cd)
  command tyr --cd-file "$dir" "$@"
  [ -s "$dir" ] && cd "$(cat "$dir")"
  rm -f "$dir"
}
```

In PowerShell:

```powershell
function tyr {
  $cd = New-TemporaryFile
  & tyr.exe --cd-file $cd.FullName @args
  $dir = (Get-Content $cd -Raw).Trim()
  Remove-Item $cd
  if ($dir) { Set-Location -LiteralPath $dir }
}
```

`$TYR_CD_FILE` works in place of the flag. A pane left on a remote host writes nothing, so the
shell stays where it was.

## Archives

`p` opens the pack dialog, `u` unpacks an archive into the other pane, and `U` unpacks it in
place. Archive actions shell out to standard CLIs, and only the tool for the format you touch
is required:

| Format                     | Extract          | Create         |
| -------------------------- | ---------------- | -------------- |
| `.tar`                     | `tar`            | `tar`          |
| `.tar.gz` / `.tgz`         | `tar`            | `tar` + `gzip` |
| `.tar.bz2` / `.tbz2`       | `tar`            | `tar` + `bzip2`|
| `.tar.xz` / `.txz`         | `tar`            | `tar` + `xz`   |
| `.tar.zst` / `.tzst`       | `tar`            | `tar` + `zstd` |
| `.zip`                     | `unzip` or 7-Zip | `zip` or 7-Zip |
| `.7z`                      | 7-Zip            | 7-Zip          |
| `.rar`                     | `unrar`          | —              |

Extract progress counts entries with `tar -t` / `zipinfo`; 7-Zip and `unrar` show an
indeterminate bar. `C` reports which of these tools are on your `PATH` right now.

Editing a member of a `.zip` also needs `zip`, and `zipinfo` lists its entries.

### Packing

`p` opens a dialog rather than packing straight away, because a `.tar.gz` at the default level
is rarely the only thing you want:

| Field      | Keys | What it is                                                                                                                |
| ---------- | ---- | ------------------------------------------------------------------------------------------------------------------------- |
| `Format`   | `←→` | `tar.gz`, `tar.bz2`, `tar.xz`, `tar.zst`, `tar`, `zip`, `7z` — only the formats this machine has the tools for are offered |
| `Level`    | `←→` | how hard to compress: `1`–`9` for gzip and bzip2, `0`–`9` for xz, `1`–`19` for zstd, `0` (store) to `9` for zip and 7z     |
| `Password` | type | encrypts the archive — `zip` and `7z` only, see below                                                                      |
| `Name`     | type | the output name; its extension follows the format until you type your own                                                  |

`↑`/`↓` (or `Tab`) move between the fields, `←`/`→` change the highlighted one, `Enter` goes on
to the usual confirm prompt — which restates the format, level and encryption before anything
is written — and `Esc` cancels. Each format opens on its own sensible level (gzip 6, bzip2 9,
xz 6, zstd 3, zip 6, 7z 5), and the level is a real setting: it reaches the compressor rather
than being decoration.

The tar formats are streamed as `tar -cvf - | <compressor>`, which is what makes the level
reachable at all — and keeps the per-file progress bar, since `tar -v` still names every file
as it goes by.

### Passwords

Encryption is 7-Zip's whenever it is installed — under any of its command names, `7z`, `7zz`
or `7za`: **AES-256** for both `.7z` and `.zip`, and in a `.7z` the file names are encrypted as
well. Without 7-Zip, a `.zip` falls back to Info-ZIP's
legacy **ZipCrypto**, which is weak — the dialog and the confirm prompt both say so, so it is
never used by accident. The tar formats compress but cannot encrypt: the password field says as
much and the cursor skips it.

Unpacking an encrypted archive needs no preparation. Press `u`, and when the tool reports that
the archive is protected, tyr asks for the password and runs the same job again with it. A
wrong password comes back to the prompt saying so; `Esc` gives up. Encrypted `.zip` files are
extracted with `7z` when it is installed, because Info-ZIP's `unzip` cannot read the AES
entries that 7-Zip and most modern zip tools write.

Passwords are never written to disk, never saved into the config, and never kept past the
dialog or prompt that collected them. One caveat worth knowing: `7z` is fed the password on
**stdin** while packing, so it stays out of the process list, but `zip -P`, `unzip -P` and
`7z x -p…` accept one only as a command-line argument — while those run, the password is
visible to other users on the same machine (`ps`, `/proc/<pid>/cmdline`). On a shared box,
prefer `.7z`.

Adding files to an archive in place (`F5`/`c` from a pane inside an archive) does not take a
password, so it works on unencrypted archives only.

Press `Enter` on any tar or `.zip` to browse it as a virtual directory tree — `Enter` and `h`
walk it, `v`/`e` open members (see below). `.7z` and `.rar` have to be unpacked to disk first.

With one pane inside an archive and the other on real files, `F5`/`c` or `F6`/`m` **adds** the
selection to the archive at its current virtual directory. `p`/`u` still need a real
destination pane.

## Capabilities

`C` opens the capabilities overlay: every packing format, unpacking format, password feature,
remote and local action, with the tool it runs and whether that tool is on your `PATH`. `✓`
works here and `✗` does not — the row names the missing binary and what to install. The header
counts what is unavailable, so a fresh box is one keypress away from telling you what to
install. `?` swaps to the keybindings and back; any other key closes.

Nothing is probed until you open the overlay, and nothing is cached — install `p7zip`, reopen
it, and the `.7z` row turns green without restarting tyr.

## View & edit

`v` opens a file in the pager, `e` opens it in a nano-style editor — `Ctrl+S` saves, `Ctrl+Q`
quits, and `Esc` guards unsaved changes.

The pager has the four switches a pager is asked for:

| Key   | What it does                                                            |
| ----- | ----------------------------------------------------------------------- |
| `/`   | find text — the file narrows to the hits as you type                     |
| `n`   | next hit; `N` the previous one, both wrapping around the file            |
| `w`   | wrap long lines instead of cutting them at the edge                      |
| `#`   | line numbers                                                             |
| `x`   | the hex dump: offset, bytes, and the characters they stand for           |
| `s`   | syntax highlighting                                                      |
| `e`   | hand this file to the editor                                             |
| `q`   | close (`Esc` clears the search first, if there is one)                   |

Everything the pager draws is built from the bytes it was handed, so each of those is a
re-render rather than a re-read — which is why **a binary file opens instead of being
refused**. It comes up in hex, and `x` switches back if you want to see it as text anyway.

Highlighting is a small built-in one rather than a dependency: it knows four shapes of
language — C-like (`//` and `/* */`, quoted strings), hash-comment (shell, Python, YAML, TOML,
Makefiles), JSON, and Markdown — and within them it finds comments, strings, numbers and
keywords, carrying a block comment or a fenced code block across the lines it spans. It is
what a reader's eye uses. It is not a parser, and it does not pretend to be one.

Both work on archive members. An edited member is written back to the archive: a targeted
update for zip and uncompressed tar, a transparent repack for compressed tar.

View and edit are local-only; copy a remote file across first. `x` runs a command and shows
what it printed in this same pager (see [The shell](#the-shell)).

## Over ssh

Press `S` for the connection modal. It lists your saved connections, most recently
used first, with a `●` next to any that are already connected:

```
╭──────────────────────────────────────────────────────────╮
│   Connect over ssh                           left pane   │
│                                                          │
│   ▸ prod         deploy@web01.example.com /srv/www       │
│     backup       ● kim@nas:2222                          │
│     + new connection…                                    │
│                                                          │
│   enter connect · n new · e edit · d delete · esc        │
╰──────────────────────────────────────────────────────────╯
```

`enter` connects, `n` adds one, `e` edits, `d` deletes. The connection opens in the
pane you pressed `S` from — the modal says which one.

A connection records a name, host, user, port, starting path, and optionally a key
file. **It never records a password.** If the host needs one, tyr asks each
time it starts:

```
╭──────────────────────────────────────────────────────────╮
│   Password                                   left pane   │
│                                                          │
│   for prod (deploy@web01.example.com)                    │
│                                                          │
│   ••••••••••                                             │
│                                                          │
│   not saved — asked again next time tyr starts           │
╰──────────────────────────────────────────────────────────╯
```

The password is offered to the server and kept in memory for that session, so
transfers and reconnects after an idle timeout do not ask again. Quitting drops it.
Authentication is tried in order: ssh-agent, then key files, then the password —
so a key-based host never prompts at all.

A host that isn't in `~/.ssh/known_hosts` shows its fingerprint for you to check
before anything is sent to it; accepting appends it to `known_hosts`, as `ssh`
would. A known host presenting a _different key of the same type_ is refused
outright rather than offered as a yes/no — that is either a rebuilt server or an
interception, and either way it wants looking at by hand. A key of an algorithm
you have no entry for is just a key you have not seen, and prompts normally.

As `ssh` does, tyr asks the server for a host key algorithm it already has
recorded, so a host whose `known_hosts` line is ed25519 is not re-verified against
whatever key type the server happens to prefer.

Saved connections live in the same config file as everything else:

```ini
# tyr configuration
# ssh passwords are never stored here
conn.prod.host = web01.example.com
conn.prod.user = deploy
conn.prod.path = /srv/www
```

### Transferring

With one pane remote and one local, `c`/`F5` and `m`/`F6` transfer in whichever
direction the panes describe — the **active** pane is always the source, so copying
down and copying up are the same two keys. `d`/`F8` deletes on the host. With both
panes on the same host, copy and move run there without the data crossing the wire.

The address bar (`Ctrl+L`) also accepts a remote target, which goes through the same
connect flow:

```
ssh://user@host:2222/var/log
user@host:/var/log            # scp-style
host:                         # the login directory
```

While a pane is remote, plain paths in the address bar stay on that host; `local:/path`
(or `file:///path`) brings it back to this machine. `Enter`/`h` walk the tree, `Space`
marks, `s` sorts, `.` toggles hidden files, and `n`/`N` create on the host.

Not available over ssh: pack/unpack, browsing into archives, and view/edit — copy the
file across first. Copying directly between two different hosts is refused; route it
through this machine.

### How it works

tyr speaks ssh in-process (`golang.org/x/crypto/ssh`) rather than shelling out
to the `ssh` binary. That is what makes the password prompt possible at all: `ssh`
reads passwords straight from the terminal, which the TUI owns, and a password passed
any other way would have to travel through a command line or an environment variable
where other processes can see it. In-process, it goes from the input straight into the
authentication exchange.

The trade-off is that a native client does not read `~/.ssh/config` for you, so
tyr parses the part that decides where a connection goes: `HostName`, `User`,
`Port`, `IdentityFile`, `ProxyJump`, plus `Include` and `Host` pattern matching. An
alias that depends on anything else — `ProxyCommand`, for instance — will not resolve
the way `ssh` would. A jump host must accept key or agent authentication, since the
modal only prompts for one password.

Transfers stream a tar archive over one ssh session (`tar -cf -` on one end, `tar -xf -`
on the other) rather than using scp or sftp. Every path is quoted by tyr and
interpreted by exactly one shell, and the local `tar -v` names each file as it moves,
which is what fills the progress bar. The far side needs `tar` and a POSIX shell;
nothing is installed.

## Themes

Eight built-in themes: `default`, `nord`, `dracula`, `gruvbox`, `catppuccin`, `tokyonight`,
`solarized`, `monokai`. Press `t` for the picker — moving the cursor previews the theme live,
`Enter` applies and remembers it, `Esc` puts the old one back.

Resolution order is `--theme` → `$TYR_THEME` → config file → `default`:

```sh
tyr --theme gruvbox
TYR_THEME=dracula tyr
```

The picker writes the choice to `$XDG_CONFIG_HOME/tyr/config` (or `~/.config/tyr/config`, or
`%AppData%\tyr\config` on Windows), a plain `key = value` file you can also edit by hand:

```ini
# tyr configuration
theme = nord
```

Saved ssh connections share this file; see [Over ssh](#over-ssh), and so does the session tyr
writes on the way out — see below.

### Your own themes

A theme is eight colours, so a theme is eight lines. Drop a `.theme` file in
`$XDG_CONFIG_HOME/tyr/themes` (or `~/.config/tyr/themes`, or `%AppData%\tyr\themes`) and it
is in the picker on the next start — sending someone a theme is sending them a file.

```sh
tyr --new-theme rose     # writes themes/rose.theme from the theme you are using
```

```ini
# ~/.config/tyr/themes/rose.theme
name     = rose          # optional; the file name is used otherwise
accent   = #D3869B       # active border, cursor, directory names, keywords
dim      = #665C54       # inactive border, faint text, comments
fg       = #FBF1C7       # status bar text
title    = #EBDBB2       # address bar, numbers in code
mark     = #FABD2F       # marked entries, strings, search hits
bar      = #3C3836       # status bar background
danger   = #FB4934       # deletes, overwrites, errors
cursorfg = #1D2021       # text drawn on top of accent
```

Colours are `#rgb`, `#rrggbb`, or an ANSI palette index from 0 to 255 — the built-in `default`
theme is made of those, which is why it follows your terminal's own palette. A file named
after a built-in replaces it, so adjusting `nord` is a file called `nord.theme` rather than a
second theme with a different name.

A file that is not a theme costs its own theme and nothing else: tyr says which file and what
was wrong with it in the status bar, and carries on with the rest.

Built-in themes are pure data too — a name plus eight colours in `internal/ui/theme.go`.

## What tyr remembers

Quitting with `q` writes each pane's sort order, hidden-file setting and directory to the same
config file:

```ini
left.path = /home/kim/src/tyr
left.sort = size
left.hidden = true
right.path = /home/kim/downloads
right.sort = name
right.hidden = false
```

The sort order and the hidden-file setting come back on the next run. The **directories do
not**, unless you ask for them:

```ini
startup = last     # reopen where you left off; anything else (or nothing) means the
                   # directory you launched tyr from
delete = remove    # make F8 unlink instead of using the trash (default: trash)
mouse = off        # ignore the mouse (default: on)
```

Rebound keys live in the same file, as `key.<action>` lines — see
[Rebinding them](#rebinding-them).

A saved directory that has since gone away is skipped rather than argued about, and a pane
that ended the run on a remote host saves no path — the connection is not restored, so neither
is the location. Nothing else about a session is written: no passwords, no selection, no
filter.

> **Upgrading from lazyfiles?** The first run moves a leftover `~/.config/lazyfiles`
> directory to `~/.config/tyr` and tells you it did, so your theme and connections carry
> over. If a `~/.config/tyr` already exists it is left alone and nothing is moved. The
> `LAZYFILES_THEME` environment variable is *not* carried over — use `TYR_THEME`.

