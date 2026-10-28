package syntax

import (
	"path/filepath"
	"strings"
)

// The label is what the editor calls a file — "bash", "c#", "makefile" — which
// is finer than the shape it is highlighted as: a .tf and a .ini scan the same
// and are different things to the person editing them.

// labelByExtension names the extensions whose language has no name of its
// own here, or a better one than its highlighting gets.
var labelByExtension = map[string]string{
	".sh": "bash", ".bash": "bash", ".zsh": "zsh", ".fish": "fish",
	".jsx": "jsx", ".tsx": "tsx", ".scala": "scala", ".scss": "scss", ".zig": "zig",
	".dart": "dart", ".proto": "protobuf", ".pl": "perl", ".r": "r", ".ex": "elixir",
	".exs": "elixir", ".ini": "ini", ".conf": "config", ".cfg": "config", ".tf": "terraform",
	".jsonc": "json with comments", ".mdx": "mdx", ".svg": "svg", ".plist": "plist",
	".csproj": "c# project", ".fsproj": "f# project", ".vcxproj": "c++ project",
	".xaml": "xaml", ".rss": "rss", ".atom": "atom", ".kts": "kotlin script",
	".h": "c header", ".hpp": "c++ header", ".hh": "c++ header",
}

// labelByName is the same for files known by name.
var labelByName = map[string]string{
	"makefile": "makefile", ".gitignore": "gitignore", ".env": "env", ".bashrc": "bash",
	".zshrc": "zsh", ".profile": "shell", "config": "config", "gemfile": "gemfile",
	"rakefile": "rakefile", "vagrantfile": "vagrantfile", "containerfile": "containerfile",
}

// interpreters maps what a #! line runs onto the language the file is.
var interpreters = map[string]Lang{
	"sh": Shell, "bash": Shell, "zsh": Shell, "fish": Shell, "dash": Shell, "ksh": Shell,
	"python": Python, "python2": Python, "python3": Python, "ruby": Ruby,
	"node": JavaScript, "deno": TypeScript, "bun": JavaScript, "perl": Hash,
}

// shebang is the program a #! first line runs: "bash" for both
// #!/bin/bash and #!/usr/bin/env bash. "" when there is no #! line.
func shebang(firstLine string) string {
	rest, ok := strings.CutPrefix(firstLine, "#!")
	if !ok {
		return ""
	}
	fields := strings.Fields(rest)
	if len(fields) == 0 {
		return ""
	}
	prog := filepath.Base(fields[0])
	if prog == "env" {
		prog = ""
		for _, f := range fields[1:] {
			if !strings.HasPrefix(f, "-") { // env -S, env -i
				prog = filepath.Base(f)
				break
			}
		}
	}
	return prog
}

// DetectText picks the language from a file's name, and failing that from a
// #! first line — a script called deploy is as much a script as deploy.sh.
func DetectText(name, firstLine string) Lang {
	if lang := Detect(name); lang != None {
		return lang
	}
	if lang, ok := interpreters[shebang(firstLine)]; ok {
		return lang
	}
	return None
}

// Label is what to call a file, from its name and first line: the #! program
// when there is one, then the name or extension, then its language.
func Label(name, firstLine string) string {
	if prog := shebang(firstLine); prog != "" {
		if _, ok := interpreters[prog]; ok {
			return strings.TrimRight(prog, "0123456789") // python3 is python
		}
	}
	base := strings.ToLower(filepath.Base(name))
	if label, ok := labelByName[base]; ok {
		return label
	}
	if label, ok := labelByExtension[filepath.Ext(base)]; ok {
		return label
	}
	if lang := DetectText(name, firstLine); lang != None {
		return lang.String()
	}
	return "plain text"
}
