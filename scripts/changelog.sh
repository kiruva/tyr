#!/usr/bin/env sh
# Reads and rewrites CHANGELOG.md, so a release does not depend on somebody
# remembering to move a heading.
#
#   changelog.sh section <version|unreleased>   print one section's body
#   changelog.sh has-entries                   exit 0 if Unreleased says something
#   changelog.sh promote <version> [date]      Unreleased becomes [version] - date
#
# The file is Keep a Changelog: sections are "## [Unreleased]" and
# "## [X.Y.Z] - YYYY-MM-DD", and nothing else in the file starts with "## ".
set -eu

file=${CHANGELOG_FILE:-CHANGELOG.md}

usage() {
	cat >&2 <<'EOF'
changelog.sh section <version|unreleased>   print one section's body
changelog.sh has-entries                    exit 0 if Unreleased says something
changelog.sh promote <version> [date]       Unreleased becomes [version] - date
EOF
	exit 2
}

# heading_of prints the "## [...]" line a version is under.
heading_of() {
	case $1 in
	unreleased | Unreleased | UNRELEASED) printf '## [Unreleased]' ;;
	*) printf '## [%s]' "${1#v}" ;;
	esac
}

# section prints the body under a heading, without the heading itself and
# without the blank lines at either end. A version with no section, or an empty
# one, is an exit code rather than silence, so a caller can tell.
section() {
	want=$(heading_of "$1")
	body=$(awk -v want="$want" '
		index($0, want) == 1 { inside = 1; next }
		inside && /^## / { exit }
		inside { body = body $0 "\n" }
		END {
			sub(/^\n+/, "", body)
			sub(/\n+$/, "", body)
			if (body != "") print body
		}
	' "$file")

	[ -n "$body" ] || return 1
	printf '%s\n' "$body"
}

# has_entries reports whether Unreleased has anything under it worth releasing.
has_entries() {
	body=$(section unreleased)
	# Sub-headings on their own ("### Added") are not entries.
	rest=$(printf '%s\n' "$body" | grep -v '^###' | grep -v '^[[:space:]]*$' || true)
	[ -n "$rest" ]
}

# promote turns the Unreleased heading into a released one and opens a fresh
# Unreleased above it. Everything else in the file is left byte for byte.
promote() {
	version=${1#v}
	date=${2:-$(date -u +%Y-%m-%d)}

	case $version in
	[0-9]*.[0-9]*.[0-9]*) ;;
	*)
		echo "changelog.sh: $version is not a X.Y.Z version" >&2
		exit 1
		;;
	esac

	if grep -q "^## \[$version\]" "$file"; then
		echo "changelog.sh: $file already has a [$version] section" >&2
		exit 1
	fi
	if ! grep -q '^## \[Unreleased\]' "$file"; then
		echo "changelog.sh: $file has no ## [Unreleased] heading to promote" >&2
		exit 1
	fi

	tmp=$(mktemp)
	awk -v version="$version" -v date="$date" '
		/^## \[Unreleased\]$/ && !done {
			print "## [Unreleased]"
			print ""
			print "## [" version "] - " date
			done = 1
			next
		}
		{ print }
	' "$file" >"$tmp"
	mv "$tmp" "$file"
}

[ $# -ge 1 ] || usage

case $1 in
section)
	[ $# -eq 2 ] || usage
	section "$2"
	;;
has-entries)
	has_entries
	;;
promote)
	[ $# -ge 2 ] || usage
	promote "$2" "${3:-}"
	;;
*) usage ;;
esac
