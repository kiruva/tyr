# Demo recording script for tyr.
# Install VHS (https://github.com/charmbracelet/vhs), then run:
#     vhs demo.tape
# It produces demo.gif, referenced by the README.
#
# The recording walks a fixture tree in /tmp so it looks the same every time:
#
#     /tmp/tyr-demo/
#     ├── backup/            (empty — the destination pane)
#     └── project/
#         ├── assets/logo.txt
#         ├── src/{main.go,util.go}
#         ├── README.md
#         └── notes.txt
#
# Cursor positions below assume that tree and the default sort (directories
# first, then names A→Z, hidden files off). Edit the fixture and the moves
# together.

Output demo.gif

Set FontSize 16
Set Width 1200
Set Height 720
Set Padding 20
Set Framerate 24
Set TypingSpeed 120ms

# Build the binary and lay out the fixture, hidden from the recording. The
# config lives in the fixture too, so recording never touches a real one.
Hide
Type "go build -o /tmp/tyr ."
Enter
Type "rm -rf /tmp/tyr-demo && mkdir -p /tmp/tyr-demo/backup /tmp/tyr-demo/project/src /tmp/tyr-demo/project/assets"
Enter
Type "cd /tmp/tyr-demo/project"
Enter
Type "echo '# tyr' > README.md && echo 'A dual-pane file manager for the terminal.' >> README.md"
Enter
Type "echo 'todo: ship v0.1.0' > notes.txt && echo 'logo' > assets/logo.txt"
Enter
Type "echo 'package main' > src/main.go && echo 'func main() {}' >> src/main.go"
Enter
Type "echo 'package main' > src/util.go"
Enter
Type "export XDG_CONFIG_HOME=/tmp/tyr-demo/config"
Enter
Type "cd /tmp/tyr-demo && clear && /tmp/tyr"
Enter
Sleep 1s
Show

# Two panes, both on the fixture root. Walk down to project/.
Sleep 1s
Down@400ms 2
Sleep 800ms
Enter
Sleep 1.2s

# Point the other pane at backup/ — that is where copies will land.
Tab
Sleep 800ms
Down@400ms 1
Enter
Sleep 1s
Tab
Sleep 1s

# Mark two files with Space (the cursor advances on its own).
Down@400ms 3
Sleep 600ms
Space
Sleep 500ms
Space
Sleep 1.2s

# Copy them to the other pane: c asks first, y confirms.
Type "c"
Sleep 2s
Type "y"
Sleep 1.5s

# New folder (N) and new file (n). A name with separators creates the
# directories on the way, so "docs/TODO.md" makes docs/ as well.
Type "N"
Sleep 1s
Type "release"
Sleep 800ms
Enter
Sleep 1.2s
Type "n"
Sleep 1s
Type "docs/TODO.md"
Sleep 800ms
Enter
Sleep 1.5s

# Pack src/ into the other pane as a .tar.gz.
Down@400ms 2
Sleep 600ms
Type "p"
Sleep 2s
Type "y"
Sleep 1.5s

# Browse the archive that just appeared, as though it were a directory. The
# cursor lands on ".." each time, so step down before descending.
Tab
Sleep 800ms
Down@400ms 3
Sleep 800ms
Enter
Sleep 1.2s
Down@400ms 1
Enter
Sleep 1.2s
Down@400ms 1
Sleep 600ms

# View a member without unpacking anything.
Type "v"
Sleep 2.5s
Type "q"
Sleep 800ms

# Back out of the archive and over to the local pane.
Type "h"
Sleep 700ms
Type "h"
Sleep 1s
Tab
Sleep 1s

# Edit a real file: e opens the editor, Ctrl+S saves, Ctrl+Q closes.
Down@400ms 1
Sleep 600ms
Type "e"
Sleep 1.2s
Type "note: edited right here"
Enter
Sleep 1s
Ctrl+S
Sleep 1.5s
Ctrl+Q
Sleep 1s

# Theme picker: the cursor previews each theme live, Enter keeps it.
Type "t"
Sleep 1.2s
Down@600ms 3
Sleep 1.2s
Enter
Sleep 1.5s

# The whole keymap, generated from the bindings themselves.
Type "?"
Sleep 3.5s
Escape
Sleep 800ms

Type "q"
Sleep 1s
