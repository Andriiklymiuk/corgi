package cmd

import (
	"fmt"
	"os"
	"sort"
	"strings"

	"andriiklymiuk/corgi/utils"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

var vocabCmd = &cobra.Command{
	Use:     "vocab <name>",
	Aliases: []string{"lookup"},
	Short:   "Explain any error code, compose field, command or flag in one answer",
	Long: `Answers for one name without reading the docs: an error code (E_PORT_CONFLICT),
a corgi-compose.yml key (depends_on_db, healthCheck, driver), a command
(run, agent watch) with every flag it takes, or a flag (--detach) with the
commands that take it. With no name, lists the error codes and the commands.

An unknown name is refused with the closest match, and exit 1.
Under --json the answer is one object with a "kind" of code, field, command or flag.`,
	Example: `corgi vocab E_READINESS_TIMEOUT
corgi vocab depends_on_db --json
corgi vocab agent watch
corgi vocab --detach`,
	Args:               cobra.ArbitraryArgs,
	DisableFlagParsing: true,
	Run:                runVocab,
}

func init() {
	rootCmd.AddCommand(vocabCmd)
}

type vocabFlag struct {
	Name      string `json:"name"`
	Shorthand string `json:"shorthand,omitempty"`
	Type      string `json:"type"`
	Default   string `json:"default,omitempty"`
	Usage     string `json:"usage"`
}

type vocabCommand struct {
	Kind        string      `json:"kind"`
	Command     string      `json:"command"`
	Short       string      `json:"short,omitempty"`
	Usage       string      `json:"usage"`
	Aliases     []string    `json:"aliases,omitempty"`
	Flags       []vocabFlag `json:"flags"`
	Subcommands []string    `json:"subcommands,omitempty"`
	Example     string      `json:"example,omitempty"`
	// AlsoField lists compose keys with the same name (run is a command and e2e.run).
	AlsoField []utils.ComposeField `json:"alsoField,omitempty"`
}

type vocabField struct {
	Kind   string               `json:"kind"`
	Name   string               `json:"name"`
	Places []utils.ComposeField `json:"places"`
}

type vocabCode struct {
	Kind string `json:"kind"`
	utils.CodeInfo
}

type vocabFlagOwners struct {
	Kind     string    `json:"kind"`
	Flag     vocabFlag `json:"flag"`
	Commands []string  `json:"commands"`
}

type vocabIndex struct {
	Kind     string           `json:"kind"`
	Codes    []utils.CodeInfo `json:"codes"`
	Commands []string         `json:"commands"`
}

func runVocab(cmd *cobra.Command, args []string) {
	var rest []string
	for _, a := range args {
		switch a {
		case "--json":
			utils.JSONOutput = true
			continue
		case "--help", "-h":
			_ = cmd.Help()
			return
		}
		rest = append(rest, a)
	}
	if len(rest) == 0 {
		printVocabIndex()
		return
	}
	answer, ok := resolveVocab(rest)
	if !ok {
		name := strings.Join(rest, " ")
		fix := "run `corgi vocab` for the index"
		if near := utils.Nearest(name, vocabNames()); near != "" {
			fix = fmt.Sprintf("did you mean %q? (corgi vocab %s)", near, near)
		}
		msg := fmt.Sprintf("corgi knows no code, compose field, command or flag named %q", name)
		if utils.JSONOutput {
			utils.JSONErrorFix(utils.ErrUsage, msg, fix)
		} else {
			fmt.Fprintf(os.Stderr, "%s\n→ %s\n", msg, fix)
		}
		exitProcess(1)
		return
	}
	if utils.JSONOutput {
		utils.PrintJSON(answer)
		return
	}
	printVocabHuman(answer)
}

func resolveVocab(args []string) (any, bool) {
	name := strings.TrimSpace(strings.Join(args, " "))
	if c, ok := utils.LookupCode(name); ok {
		return vocabCode{Kind: "code", CodeInfo: c}, true
	}
	if strings.HasPrefix(name, "-") {
		return lookupFlagOwners(strings.TrimLeft(name, "-"))
	}
	places := utils.LookupComposeField(name)
	if name == "driver" {
		for i := range places {
			places[i].Enum = utils.KnownDrivers
		}
	}
	if name == "corgi" {
		return describeCommand(rootCmd), true
	}
	if found, rest, err := rootCmd.Find(args); err == nil && found != rootCmd && len(rest) == 0 {
		d := describeCommand(found)
		d.AlsoField = places
		return d, true
	}
	if len(places) > 0 {
		return vocabField{Kind: "field", Name: name, Places: places}, true
	}
	return nil, false
}

func describeCommand(c *cobra.Command) vocabCommand {
	out := vocabCommand{Kind: "command", Command: c.CommandPath(), Short: c.Short, Usage: c.UseLine(), Aliases: c.Aliases, Example: c.Example}
	flags := c.LocalFlags()
	flags.VisitAll(func(f *pflag.Flag) {
		if f.Hidden || f.Name == "help" {
			return
		}
		out.Flags = append(out.Flags, flagInfo(f))
	})
	if out.Flags == nil {
		out.Flags = []vocabFlag{}
	}
	for _, sub := range c.Commands() {
		if sub.IsAvailableCommand() {
			out.Subcommands = append(out.Subcommands, sub.Name())
		}
	}
	return out
}

func flagInfo(f *pflag.Flag) vocabFlag {
	def := f.DefValue
	if def == "false" || def == "[]" || def == "0" {
		def = ""
	}
	return vocabFlag{Name: "--" + f.Name, Shorthand: f.Shorthand, Type: f.Value.Type(), Default: def, Usage: f.Usage}
}

func lookupFlagOwners(name string) (any, bool) {
	var owners []string
	var info *vocabFlag
	walkCommands(rootCmd, func(c *cobra.Command) {
		f := c.LocalFlags().Lookup(name)
		if f == nil || f.Hidden {
			return
		}
		if info == nil {
			fi := flagInfo(f)
			info = &fi
		}
		owners = append(owners, c.CommandPath())
	})
	if info == nil {
		return nil, false
	}
	return vocabFlagOwners{Kind: "flag", Flag: *info, Commands: owners}, true
}

func walkCommands(c *cobra.Command, fn func(*cobra.Command)) {
	if c.Hidden {
		return
	}
	fn(c)
	for _, sub := range c.Commands() {
		walkCommands(sub, fn)
	}
}

func vocabNames() []string {
	var names []string
	for _, c := range utils.ErrorCatalog {
		names = append(names, c.Code)
	}
	names = append(names, utils.ComposeFieldNames()...)
	walkCommands(rootCmd, func(c *cobra.Command) {
		if c != rootCmd {
			names = append(names, strings.TrimPrefix(c.CommandPath(), "corgi "))
		}
		c.LocalFlags().VisitAll(func(f *pflag.Flag) {
			if !f.Hidden {
				names = append(names, "--"+f.Name)
			}
		})
	})
	return names
}

func printVocabIndex() {
	var cmds []string
	for _, c := range rootCmd.Commands() {
		if c.IsAvailableCommand() {
			cmds = append(cmds, c.Name())
		}
	}
	sort.Strings(cmds)
	if utils.JSONOutput {
		utils.PrintJSON(vocabIndex{Kind: "index", Codes: utils.ErrorCatalog, Commands: cmds})
		return
	}
	fmt.Println("Error codes (corgi vocab <code>):")
	for _, c := range utils.ErrorCatalog {
		fmt.Printf("  %-24s %s\n", c.Code, c.Meaning)
	}
	fmt.Println("\nCommands (corgi vocab <command> [<subcommand>]):")
	fmt.Println("  " + strings.Join(cmds, " · "))
	fmt.Println("\nCompose fields: corgi vocab <field>, e.g. depends_on_db, healthCheck, driver")
	fmt.Println("Flags: corgi vocab --<flag>, e.g. --detach")
}

func printVocabHuman(answer any) {
	switch a := answer.(type) {
	case vocabCode:
		kind := "error"
		if a.Warning {
			kind = "warning"
		}
		fmt.Printf("%s (%s)\n  %s\n  fix: %s\n", a.Code, kind, a.Meaning, a.Fix)
	case vocabField:
		printVocabField(a)
	case vocabCommand:
		printVocabCommand(a)
	case vocabFlagOwners:
		fmt.Printf("%s %s - %s\n  taken by: %s\n", a.Flag.Name, a.Flag.Type, vocabFirstLine(a.Flag.Usage), strings.Join(a.Commands, ", "))
	}
}

func printVocabField(a vocabField) {
	for _, p := range a.Places {
		fmt.Printf("%s  %s\n", p.Path, p.Type)
		if p.Description != "" {
			fmt.Printf("  %s\n", p.Description)
		}
		if len(p.Enum) > 0 {
			fmt.Printf("  one of: %s\n", strings.Join(p.Enum, ", "))
		}
	}
}

func printVocabCommand(a vocabCommand) {
	fmt.Printf("%s - %s\n  usage: %s\n", a.Command, a.Short, a.Usage)
	if len(a.Aliases) > 0 {
		fmt.Printf("  aliases: %s\n", strings.Join(a.Aliases, ", "))
	}
	for _, f := range a.Flags {
		name := f.Name
		if f.Shorthand != "" {
			name = "-" + f.Shorthand + ", " + name
		}
		def := ""
		if f.Default != "" {
			def = " (default " + f.Default + ")"
		}
		fmt.Printf("  %-28s %s%s\n", name+" "+f.Type, vocabFirstLine(f.Usage), def)
	}
	if len(a.Subcommands) > 0 {
		fmt.Printf("  subcommands: %s\n", strings.Join(a.Subcommands, " · "))
	}
	for _, p := range a.AlsoField {
		fmt.Printf("  also a compose key: %s  %s\n", p.Path, p.Type)
	}
}

func vocabFirstLine(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return s[:i]
	}
	return s
}
