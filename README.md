# tyr

> Two panes. One keyboard. Move files fast.

[![CI](https://github.com/kiruva/tyr/actions/workflows/ci.yml/badge.svg)](https://github.com/kiruva/tyr/actions/workflows/ci.yml)
[![Go Report Card](https://goreportcard.com/badge/github.com/kiruva/tyr)](https://goreportcard.com/report/github.com/kiruva/tyr)
[![License: MIT](https://img.shields.io/badge/License-MIT-blue.svg)](LICENSE)

![demo](demo.gif)

## Why tyr

Mouse slow. Menus slower. tyr put two folders side by side. Active pane is source, other pane
is target, `Tab` swap them. That is whole mental model.

Total Commander spirit, terminal body. Built for Linux first, happy on macOS, runs on Windows
too. Keyboard driven, intuitive like lazygit.

## What tyr do

- **Two panes, one direction.** Copy, move, compare between them. Direction always what `Tab`
  say.
- **Find your stuff.** `/` shrink pane while you type. `Ctrl+F` hunt whole tree by name and by
  text inside files.
- **Move big piles.** Mark all, invert, mark by mask. Jobs run off UI thread with live progress
  bar. Name already taken? Ask once, answer for rest of batch.
- **Nothing lost by accident.** `F8` send to desktop trash, `Ctrl+Z` bring back. Same key undo
  move, copy, rename. Twenty steps deep.
- **Rename whole pile.** `M` open batch tool: regex, globs, name masks, counters, dates. Live
  preview of every old name and new name before disk touched.
- **Sync two trees.** `F9` compare both panes, propose direction for every difference, run only
  what you agree to.
- **Archives like folders.** Pack tar, zip, 7z with compression level and password (AES-256
  where 7-Zip installed). Unpack tar, zip, 7z, rar. Walk inside archive, add files without
  unpacking.
- **Read and write files.** Pager with search, wrap, line numbers, syntax colors, hex dump for
  binary. Nano-style editor saves in place, even file inside archive.
- **Remote panes.** One pane on ssh host, other local, transfer both ways. Reads
  `~/.ssh/config` aliases, uses agent and keys, checks host keys. Passwords never written to
  disk.
- **Your keys, your colors.** Every action rebindable from inside app. Eight themes, live
  preview picker, own theme is eight colors in one file.
- **Remembers.** Sort order and hidden-file setting come back next run. Ask nice and folders
  come back too.

## Install

```sh
go install github.com/kiruva/tyr@latest    # needs Go 1.26 or newer
```

Prebuilt Linux, macOS and Windows binaries (amd64/arm64) hang on every
[release](https://github.com/kiruva/tyr/releases). Windows ships a `.zip`, rest ship
`.tar.gz`.

From source:

```sh
git clone https://github.com/kiruva/tyr
cd tyr
make build     # makes ./tyr
```

Binary alone enough for browsing, copying, editing. Archives and ssh transfers call standard
system tools. Press `C` inside tyr to see what this machine has and what missing.

Windows works best in Windows Terminal. Few things differ there, see
[MAN.md](MAN.md#on-windows).

## Start here

```sh
tyr
```

| Key                | Does                          |
| ------------------ | ----------------------------- |
| `Tab`              | Swap active pane              |
| `j` / `k`          | Down / up                     |
| `Enter` / `h`      | Enter folder / go up          |
| `/`                | Filter this pane              |
| `Ctrl+F`           | Find below this folder        |
| `Space`            | Mark entry                    |
| `F5` / `c`         | Copy to other pane            |
| `F6` / `m`         | Move to other pane            |
| `F8` / `d`         | Delete to trash               |
| `Ctrl+Z`           | Undo last thing               |
| `M`                | Batch rename                  |
| `F9`               | Compare and sync panes        |
| `?`                | Show all keys                 |
| `q`                | Quit                          |

Forget a key? Press `?`. Every key rebindable.

## Full manual

[MAN.md](MAN.md) holds all of it: every key, every dialog, rename recipes, archive passwords,
ssh setup, themes, config file.

## Status

Pre-1.0, moving fast. See [CHANGELOG.md](CHANGELOG.md).

## Development

```sh
make run     # go run .
make test    # go test ./...
make vet     # go vet ./...
make lint    # golangci-lint run
make build   # build ./tyr
```

Layout and guidelines in [CONTRIBUTING.md](CONTRIBUTING.md). Demo GIF regenerated with
[VHS](https://github.com/charmbracelet/vhs): `vhs demo.tape`.

## Built with

[Bubble Tea](https://github.com/charmbracelet/bubbletea) ·
[Lip Gloss](https://github.com/charmbracelet/lipgloss) ·
[Bubbles](https://github.com/charmbracelet/bubbles)

## License

[MIT](LICENSE)
