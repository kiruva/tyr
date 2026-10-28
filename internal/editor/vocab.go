package editor

import (
	"strings"

	"github.com/kiruva/tyr/internal/syntax"
)

// What completion knows about a language before anything is typed in the
// file: its keywords (from the highlighter), the commands and names that are
// not keywords but get typed all day, and a few snippets.
//
// A snippet's body marks where the cursor lands with ‸, and each tab in it
// is one level of the file's indent, under the line it is typed on.

// cursorMark is where the cursor goes once a snippet is in.
const cursorMark = "‸"

// snippet is a named piece of boilerplate.
type snippet struct {
	name, detail, body string
}

// extraWords are names worth completing that the highlighter does not colour.
var extraWords = map[syntax.Lang]string{
	syntax.Shell: `printf read cd pwd test mkdir rmdir rm cp mv ls cat grep sed awk find xargs
		sort uniq head tail cut tr wc tee curl wget chmod chown ln basename dirname date sleep
		kill ps git docker tar gzip gunzip zip unzip ssh scp rsync which command getopts shopt
		mapfile readarray wait jobs pushd popd dirs umask ulimit trap printenv env sudo systemctl
		journalctl tput stat touch realpath readlink mktemp seq jq xdg-open nohup pipefail errexit
		nounset`,
	syntax.Dockerfile: `apt-get update install apk add builder CMD-SHELL`,
	syntax.YAML: `services image build context dockerfile container_name ports volumes
		environment env_file depends_on restart unless-stopped always networks command
		entrypoint healthcheck test interval timeout retries labels deploy resources limits
		apiVersion kind metadata name namespace spec containers replicas selector matchLabels
		template containerPort args value valueFrom configMapKeyRef secretKeyRef on jobs
		runs-on steps uses with run needs strategy matrix`,
	syntax.Python: `print len range enumerate zip open isinstance list dict set tuple str int
		float bool super property staticmethod classmethod __init__ __name__ __main__ sorted
		reversed map filter sum min max abs any all format input exception ValueError
		TypeError KeyError`,
	syntax.Go: `append cap close complete copy delete len make new panic print println recover
		error string int int64 uint8 byte rune float64 bool any context fmt errors strings
		Println Printf Sprintf Errorf`,
	syntax.CSharp: `Console WriteLine ReadLine List Dictionary Task IEnumerable Exception
		ToString Count Length Add Remove Contains`,
	syntax.Markdown: ``,
}

// snippets per language, offered under their name.
var snippets = map[syntax.Lang][]snippet{
	syntax.Shell: {
		{"if", "if … then … fi", "if [[ ‸ ]]; then\n\t\nfi"},
		{"ifelse", "if … else … fi", "if [[ ‸ ]]; then\n\t\nelse\n\t\nfi"},
		{"for", "for … in … done", "for ‸ in ; do\n\t\ndone"},
		{"while", "while … done", "while ‸; do\n\t\ndone"},
		{"case", "case … esac", "case ‸ in\n\t*)\n\t\t;;\nesac"},
		{"function", "name() { … }", "‸() {\n\t\n}"},
		{"shebang", "#!/usr/bin/env bash", "#!/usr/bin/env bash\nset -euo pipefail\n\n‸"},
		{"main", "script entry point", "main() {\n\t‸\n}\n\nmain \"$@\""},
	},
	syntax.Dockerfile: {
		{"FROM", "FROM image AS name", "FROM ‸ AS build"},
		{"apt", "apt-get install, cleaned", "RUN apt-get update \\\n\t&& apt-get install -y --no-install-recommends ‸ \\\n\t&& rm -rf /var/lib/apt/lists/*"},
		{"HEALTHCHECK", "HEALTHCHECK CMD …", "HEALTHCHECK --interval=30s --timeout=3s CMD ‸ || exit 1"},
		{"ENTRYPOINT", "exec form", "ENTRYPOINT [\"‸\"]"},
	},
	syntax.Markdown: {
		{"table", "two-column table", "| ‸ |  |\n| --- | --- |\n|  |  |"},
		{"code", "fenced code block", "```‸\n\n```"},
		{"link", "[text](url)", "[‸]()"},
		{"image", "![alt](url)", "![‸]()"},
		{"todo", "- [ ] task", "- [ ] ‸"},
		{"details", "collapsible section", "<details>\n<summary>‸</summary>\n\n</details>"},
	},
	syntax.YAML: {
		{"service", "compose service", "‸:\n\timage: \n\trestart: unless-stopped"},
		{"healthcheck", "compose healthcheck", "healthcheck:\n\ttest: [\"CMD\", \"‸\"]\n\tinterval: 30s\n\ttimeout: 5s\n\tretries: 3"},
	},
	syntax.Python: {
		{"main", "if __name__ == …", "if __name__ == \"__main__\":\n\t‸"},
		{"def", "def name(): …", "def ‸():\n\t"},
		{"for", "for … in …:", "for ‸ in :\n\t"},
	},
	syntax.Go: {
		{"iferr", "if err != nil { … }", "if err != nil {\n\treturn ‸err\n}"},
		{"func", "func name() { … }", "func ‸() {\n\t\n}"},
	},
}

// vocabulary is everything a language offers, split into plain words and
// snippets. The words are built once per language.
type vocabulary struct {
	words    []string
	snippets []snippet
}

var vocabCache = map[syntax.Lang]vocabulary{}

func vocabFor(lang syntax.Lang) vocabulary {
	if v, ok := vocabCache[lang]; ok {
		return v
	}
	v := vocabulary{snippets: snippets[lang]}
	if lang != syntax.None && lang != syntax.Markdown {
		v.words = append(lang.Keywords(), strings.Fields(extraWords[lang])...)
	}
	vocabCache[lang] = v
	return v
}
