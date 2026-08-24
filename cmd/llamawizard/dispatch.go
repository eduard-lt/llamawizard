package main

import (
	"errors"
	"fmt"
	"os"
	"strings"
)

// flagAliases maps dash flag forms to the canonical command they behave
// as, so `--version`/`-v` and `--help`/`-h` keep working alongside their
// word forms.
var flagAliases = map[string]string{
	"--version": "version",
	"-v":        "version",
	"--help":    "help",
	"-h":        "help",
}

// resolve turns raw CLI arguments into a canonical command path (e.g.
// "models.add"), the remaining arguments, and whether the interactive
// wizard should run instead. Zero arguments mean the wizard; any
// unrecognized command or subcommand is an error carrying a "Did you
// mean" suggestion when a close match exists.
func resolve(args []string) (path string, rest []string, wizard bool, err error) {
	if len(args) == 0 {
		return "", nil, true, nil
	}

	if canonical, ok := flagAliases[args[0]]; ok {
		return canonical, args[1:], false, nil
	}

	cmd, ok := findCommand(commandTree, args[0])
	if !ok {
		suggestion := didYouMean(args[0], commandNames(commandTree), 2)
		return "", nil, false, unknownCommandError(
			fmt.Sprintf("command '%s'", args[0]), suggestion, "help")
	}

	if len(cmd.Children) == 0 {
		return cmd.Name, args[1:], false, nil
	}

	if len(args) < 2 {
		// Noun with no subcommand: the run function prints its own usage.
		return cmd.Name, []string{}, false, nil
	}

	sub, ok := findCommand(cmd.Children, args[1])
	if !ok {
		suggestion := didYouMean(args[1], commandNames(cmd.Children), 2)
		if suggestion != "" {
			suggestion = cmd.Name + " " + suggestion
		}
		return "", nil, false, unknownCommandError(
			fmt.Sprintf("subcommand '%s %s'", cmd.Name, args[1]), suggestion, "help "+cmd.Name)
	}

	return cmd.Name + "." + sub.Name, args[2:], false, nil
}

// findCommand returns the first command in cmds whose Name or one of its
// Aliases matches input.
func findCommand(cmds []*command, input string) (*command, bool) {
	for _, c := range cmds {
		if c.Name == input {
			return c, true
		}
		for _, a := range c.Aliases {
			if a == input {
				return c, true
			}
		}
	}
	return nil, false
}

// commandNames returns the canonical names of cmds, in slice order, for
// use as "Did you mean" candidates.
func commandNames(cmds []*command) []string {
	names := make([]string, len(cmds))
	for i, c := range cmds {
		names[i] = c.Name
	}
	return names
}

// unknownCommandError formats the multi-line error shown for an
// unrecognized command or subcommand. what is the quoted offender (e.g.
// "command 'model'" or "subcommand 'models ad'"), suggestion is the
// closest known form ("" when nothing is close enough), and usage is the
// help invocation to point the user at (e.g. "help" or "help models").
func unknownCommandError(what, suggestion, usage string) error {
	var b strings.Builder
	fmt.Fprintf(&b, "error: unknown %s\n", what)
	if suggestion != "" {
		fmt.Fprintf(&b, "Did you mean '%s'?\n", suggestion)
	}
	fmt.Fprintf(&b, "Run 'llamawizard %s' for usage.\n", usage)
	return errors.New(b.String())
}

// runResolved dispatches a canonical command path to its run function.
// rest carries the arguments that follow the subcommand (or the command
// itself for leaf commands). For nouns the subcommand name is prepended
// to rest, so the run functions' own switches keep working.
func runResolved(path string, rest []string) {
	noun, sub, hasSub := strings.Cut(path, ".")
	if hasSub {
		rest = append([]string{sub}, rest...)
	}
	switch noun {
	case "status":
		runStatus()
	case "doctor":
		runDoctor()
	case "start":
		runStart()
	case "stop":
		runStop()
	case "restart":
		runRestart()
	case "warlock":
		runWarlock(rest)
	case "logs":
		runLogsCmd(rest)
	case "models":
		runModels(rest)
	case "config":
		runConfig(rest)
	case "pi":
		runPi(rest)
	case "version":
		runVersion()
	case "update":
		runUpdate()
	case "uninstall":
		runUninstall()
	case "help":
		if len(rest) == 0 {
			printHelp()
			return
		}
		if !printCommandHelp(rest[0]) {
			os.Exit(1)
		}
	}
}
