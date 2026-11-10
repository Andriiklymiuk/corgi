package cmd

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"andriiklymiuk/corgi/utils"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

const (
	corgiCommandPrefix = "corgi "
)

var docsCommandsCmd = &cobra.Command{
	Use:   "commands [<file or dir>…]",
	Short: "Every corgi command and flag named in Markdown exists in this binary",
	Long: `Reads each corgi invocation in the code of Markdown files (inline code and
fenced blocks) and checks it against this binary: the command path exists, and
every --flag is one that command takes. A skill or guide that names a flag the
CLI does not parse teaches an agent a command that fails.

With no paths it reads ./docs, ./plugins and README.md. Exit 1 on a finding,
each with the nearest real command or flag.

  corgi docs commands
  corgi docs commands plugins/corgi/skills --json`,
	Run: func(cmd *cobra.Command, args []string) {
		if len(args) == 0 {
			args = []string{"docs", "plugins", "README.md"}
		}
		files, err := markdownFiles(args)
		if err != nil {
			exitWithError(utils.ErrUsage, err, 2)
			return
		}
		findings := checkCommandMentions(files)
		if utils.JSONOutput {
			utils.PrintJSON(map[string]any{"ok": len(findings) == 0, "files": len(files), "findings": findings})
		} else {
			for _, f := range findings {
				fmt.Printf("%s:%d  %s - %s", f.File, f.Line, f.Mention, f.Problem)
				if f.Fix != "" {
					fmt.Printf(" → %s", f.Fix)
				}
				fmt.Println()
			}
			fmt.Printf("%d file(s), %d finding(s)\n", len(files), len(findings))
		}
		if len(findings) > 0 {
			exitProcess(1)
		}
	},
}

func init() {
	docsCmd.AddCommand(docsCommandsCmd)
}

type commandFinding struct {
	File    string `json:"file"`
	Line    int    `json:"line"`
	Mention string `json:"mention"`
	Problem string `json:"problem"`
	Fix     string `json:"fix,omitempty"`
}

func markdownFiles(paths []string) ([]string, error) {
	var out []string
	for _, p := range paths {
		info, err := os.Stat(p)
		if err != nil {
			if len(paths) > 1 {
				continue
			}
			return nil, err
		}
		if !info.IsDir() {
			out = append(out, p)
			continue
		}
		_ = filepath.WalkDir(p, func(path string, d os.DirEntry, err error) error {
			if err != nil {
				return nil
			}
			if d.IsDir() && (d.Name() == "node_modules" || d.Name() == ".git") {
				return filepath.SkipDir
			}
			if !d.IsDir() && strings.HasSuffix(d.Name(), ".md") {
				out = append(out, path)
			}
			return nil
		})
	}
	sort.Strings(out)
	return out, nil
}

var (
	inlineCode = regexp.MustCompile("`([^`]+)`")
	// corgi at a word start, not corgi-compose.yml or corgi_services.
	corgiCall = regexp.MustCompile(`(?:^|[\s(;|&$])corgi((?:\s+(?:"[^"]*"|'[^']*'|[^\s|;&)"']+)+)*)`)
)

func checkCommandMentions(files []string) []commandFinding {
	// cobra adds help and completion only when it executes; a test never does.
	rootCmd.InitDefaultHelpCmd()
	rootCmd.InitDefaultCompletionCmd()
	var out []commandFinding
	for _, file := range files {
		out = append(out, checkFileMentions(file)...)
	}
	return out
}

type fenceState struct {
	inFence, shellFence bool
}

func (f *fenceState) snippets(line string) []string {
	if t := strings.TrimSpace(line); strings.HasPrefix(t, "```") {
		f.inFence = !f.inFence
		f.shellFence = f.inFence && isShellFence(strings.TrimPrefix(t, "```"))
		return nil
	}
	if f.inFence {
		if f.shellFence {
			return []string{stripComment(line)}
		}
		return nil
	}
	var snippets []string
	for _, m := range inlineCode.FindAllStringSubmatch(line, -1) {
		snippets = append(snippets, m[1])
	}
	return snippets
}

func checkFileMentions(file string) []commandFinding {
	f, err := os.Open(file)
	if err != nil {
		return nil
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64<<10), 1<<20)
	var out []commandFinding
	var fence fenceState
	for lineNo := 1; sc.Scan(); lineNo++ {
		for _, s := range fence.snippets(sc.Text()) {
			out = append(out, checkSnippet(s, file, lineNo)...)
		}
	}
	return out
}

func checkSnippet(s, file string, lineNo int) []commandFinding {
	var out []commandFinding
	for _, m := range corgiCall.FindAllStringSubmatch(s, -1) {
		if fd, bad := checkInvocation(shellWords(m[1])); bad {
			fd.File, fd.Line = file, lineNo
			out = append(out, fd)
		}
	}
	return out
}

// shellWords splits like a shell: a quoted argument is one word, kept with its
// quotes so it is never mistaken for a command or a flag.
func shellWords(s string) []string {
	var out []string
	var cur strings.Builder
	quote := byte(0)
	for i := 0; i < len(s); i++ {
		ch := s[i]
		switch {
		case quote != 0:
			cur.WriteByte(ch)
			if ch == quote {
				quote = 0
			}
		case ch == '"' || ch == '\'':
			quote = ch
			cur.WriteByte(ch)
		case ch == ' ' || ch == '\t':
			if cur.Len() > 0 {
				out = append(out, cur.String())
				cur.Reset()
			}
		default:
			cur.WriteByte(ch)
		}
	}
	if cur.Len() > 0 {
		out = append(out, cur.String())
	}
	return out
}

// Untagged fences hold commands as often as diagrams, so only their lines that
// start with corgi count; text, yaml and json fences are never commands.
func isShellFence(tag string) bool {
	switch strings.ToLower(strings.TrimSpace(tag)) {
	case "", "bash", "sh", "shell", "zsh", "console", "terminal":
		return true
	}
	return false
}

func stripComment(line string) string {
	t := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(line), "$ "))
	if !strings.HasPrefix(t, corgiCommandPrefix) && t != "corgi" && !strings.Contains(t, "&& corgi") && !strings.Contains(t, "| corgi") {
		return ""
	}
	if i := strings.Index(t, " #"); i >= 0 {
		t = t[:i]
	}
	return t
}

// checkInvocation walks the words after "corgi" down the command tree, then
// checks each --flag against the command it reached. Placeholders, comments and
// anything after a -- or a quoted argument end the walk, so prose stays legal.
func checkInvocation(words []string) (commandFinding, bool) {
	c, i, fd, bad := walkSubcommands(words)
	if bad {
		return fd, true
	}
	if c.DisableFlagParsing {
		return commandFinding{}, false
	}
	return checkFlags(c, words[i:])
}

func walkSubcommands(words []string) (*cobra.Command, int, commandFinding, bool) {
	c := rootCmd
	path := []string{"corgi"}
	i := skipGlobalFlags(words)
	start := i
	for ; i < len(words); i++ {
		w := words[i]
		if w == "#" || strings.HasPrefix(w, "-") || !isPlainWord(w) {
			break
		}
		next := subcommand(c, w)
		if next == nil {
			fd, bad := unknownSubcommand(c, path, words, i, start)
			return c, i, fd, bad
		}
		c = next
		path = append(path, w)
	}
	return c, i, commandFinding{}, false
}

func unknownSubcommand(c *cobra.Command, path, words []string, i, start int) (commandFinding, bool) {
	w := words[i]
	switch {
	case i+1 < len(words) && looksLikeProse(words[i+1]):
		return commandFinding{}, false
	case c == rootCmd && i == start:
		near := utils.Nearest(w, commandNames(rootCmd))
		return commandFinding{Mention: corgiCommandPrefix + w, Problem: "no such command", Fix: suggestion(near, corgiCommandPrefix)}, true
	case len(c.Commands()) > 0 && !c.Runnable():
		near := utils.Nearest(w, commandNames(c))
		return commandFinding{Mention: strings.Join(append(path, w), " "), Problem: "no such subcommand", Fix: suggestion(near, c.CommandPath()+" ")}, true
	}
	return commandFinding{}, false
}

func checkFlags(c *cobra.Command, words []string) (commandFinding, bool) {
	for _, w := range words {
		if w == "--" || strings.HasPrefix(w, "#") {
			break
		}
		if !strings.HasPrefix(w, "--") || len(w) < 3 {
			continue
		}
		name, _, _ := strings.Cut(strings.TrimPrefix(w, "--"), "=")
		name = strings.TrimRight(name, ".,:'\"`]")
		if name == "" || !isFlagName(name) || takesFlag(c, name) {
			continue
		}
		return commandFinding{Mention: c.CommandPath() + " --" + name, Problem: "the command takes no such flag", Fix: suggestion(utils.Nearest("--"+name, acceptedFlags(c)), "")}, true
	}
	return commandFinding{}, false
}

// skipGlobalFlags steps over root flags written before the command
// (corgi --json run, corgi --isolate run-7 db -u).
func skipGlobalFlags(words []string) int {
	i := 0
	for i < len(words) && strings.HasPrefix(words[i], "--") {
		name, _, hasValue := strings.Cut(strings.TrimPrefix(words[i], "--"), "=")
		f := rootCmd.PersistentFlags().Lookup(name)
		if f == nil {
			return i
		}
		i++
		if !hasValue && f.Value.Type() != "bool" && i < len(words) {
			i++
		}
	}
	return i
}

// A word after an unknown name that no command line would carry, such as the
// "(pid" of corgi agent running (pid 84639) or the "·" of a prompt trail, means
// the snippet is output or prose.
func looksLikeProse(next string) bool {
	return !isPlainWord(next) && !strings.HasPrefix(next, "-") && !strings.HasPrefix(next, "<") &&
		!strings.HasPrefix(next, "\"") && !strings.HasPrefix(next, "'")
}

func suggestion(near, prefix string) string {
	if near == "" {
		return ""
	}
	return "did you mean " + prefix + near + "?"
}

var plainWord = regexp.MustCompile(`^[a-z][a-z0-9-]*$`)
var flagWord = regexp.MustCompile(`^[a-zA-Z][a-zA-Z0-9-]*$`)

func isPlainWord(w string) bool { return plainWord.MatchString(w) }
func isFlagName(w string) bool  { return flagWord.MatchString(w) }

func subcommand(c *cobra.Command, name string) *cobra.Command {
	for _, sub := range c.Commands() {
		if sub.Name() == name || sub.HasAlias(name) {
			return sub
		}
	}
	return nil
}

func commandNames(c *cobra.Command) []string {
	var out []string
	for _, sub := range c.Commands() {
		out = append(out, sub.Name())
	}
	return out
}

func takesFlag(c *cobra.Command, name string) bool {
	if name == "help" || (name == "version" && c == rootCmd) {
		return true
	}
	found := false
	check := func(f *pflag.Flag) {
		if f.Name == name {
			found = true
		}
	}
	c.LocalFlags().VisitAll(check)
	c.InheritedFlags().VisitAll(check)
	for p := c; p != nil && !found; p = p.Parent() {
		p.PersistentFlags().VisitAll(check)
	}
	return found
}
