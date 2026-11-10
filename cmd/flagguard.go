package cmd

import (
	"errors"
	"fmt"
	"os"
	"sort"
	"strings"

	"andriiklymiuk/corgi/utils"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

// flagRefusal turns pflag's bare "unknown flag: --x" into a refusal that says
// what to type instead: the nearest flag the command takes, and the rest of
// them. Under --json it is the E_USAGE envelope, so an agent never has to
// parse usage text to recover.
func flagRefusal(c *cobra.Command, err error) error {
	name := unknownFlagName(err)
	if name == "" {
		return err
	}
	accepted := acceptedFlags(c)
	fix := fmt.Sprintf("`%s` takes: %s (`corgi vocab %s`)", c.CommandPath(), strings.Join(accepted, " "),
		strings.TrimPrefix(c.CommandPath(), "corgi "))
	if near := utils.Nearest(name, accepted); near != "" {
		fix = fmt.Sprintf("did you mean %s? %s", near, fix)
	}
	if jsonRequested() {
		utils.JSONErrorFix(utils.ErrUsage, err.Error(), fix)
	} else {
		fmt.Fprintf(os.Stderr, "Error: %s\n→ %s\n", err, fix)
	}
	exitProcess(2)
	return nil
}

func unknownFlagName(err error) string {
	var notExist *pflag.NotExistError
	if errors.As(err, &notExist) {
		return "--" + strings.TrimLeft(notExist.GetSpecifiedName(), "-")
	}
	msg := err.Error()
	for _, prefix := range []string{"unknown flag: ", "unknown shorthand flag: "} {
		if strings.HasPrefix(msg, prefix) {
			f := strings.Fields(strings.TrimPrefix(msg, prefix))
			if len(f) > 0 {
				return f[0]
			}
		}
	}
	return ""
}

func acceptedFlags(c *cobra.Command) []string {
	seen := map[string]bool{}
	var out []string
	add := func(f *pflag.Flag) {
		if f.Hidden || f.Name == "help" || seen[f.Name] {
			return
		}
		seen[f.Name] = true
		out = append(out, "--"+f.Name)
	}
	c.LocalFlags().VisitAll(add)
	c.InheritedFlags().VisitAll(func(f *pflag.Flag) {
		if f.Name == "json" || f.Name == "filename" {
			add(f)
		}
	})
	sort.Strings(out)
	return out
}

// --json may itself sit after the unknown flag, so pflag never set it.
func jsonRequested() bool {
	if utils.JSONOutput {
		return true
	}
	for _, a := range os.Args[1:] {
		if a == "--" {
			break
		}
		if a == "--json" {
			return true
		}
	}
	return false
}

func init() {
	rootCmd.SetFlagErrorFunc(flagRefusal)
}
