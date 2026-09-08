package main

import (
	"fmt"
	"os"
)

// completionFor returns the shell completion script for shell, or an error
// for an unknown shell. The scripts are hand-written rc-file snippets (the
// user appends them to their shell config — no eval wrapper): the command
// tree is static and small, so no completion framework is needed.
func completionFor(shell string) (string, error) {
	switch shell {
	case "bash":
		return bashCompletionScript, nil
	case "zsh":
		return zshCompletionScript, nil
	case "fish":
		return fishCompletionScript, nil
	default:
		return "", fmt.Errorf("unknown shell %q", shell)
	}
}

// runCompletion prints the completion script for the requested shell to
// stdout. With no shell it prints usage to stderr and exits 1; with an
// unrecognized shell it prints the error and the available shells to stderr
// and exits 1.
func runCompletion(args []string) {
	if len(args) == 0 {
		fmt.Fprintln(os.Stderr, "Usage: llamawizard completion <bash|zsh|fish>")
		os.Exit(1)
	}
	script, err := completionFor(args[0])
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: unknown shell '%s'\n", args[0])
		fmt.Fprintln(os.Stderr, "Available: bash, zsh, fish")
		os.Exit(1)
	}
	fmt.Print(script)
}

const bashCompletionScript = `_llamawizard() {
    local cur
    cur="${COMP_WORDS[COMP_CWORD]}"

    if [ "${COMP_CWORD}" -eq 1 ]; then
        COMPREPLY=( $(compgen -W "status st doctor dr logs lg start stop restart re warlock wl models m config cfg pi completion update up uninstall version v help h --help -h --version -v" -- "$cur") )
        return 0
    fi

    if [ "${COMP_CWORD}" -eq 2 ]; then
        case "${COMP_WORDS[1]}" in
            models|m)
                COMPREPLY=( $(compgen -W "list ls add a show sh remove rm delete" -- "$cur") )
                ;;
            config|cfg)
                COMPREPLY=( $(compgen -W "show sh path p apply" -- "$cur") )
                ;;
            pi)
                COMPREPLY=( $(compgen -W "install uninstall" -- "$cur") )
                ;;
            completion)
                COMPREPLY=( $(compgen -W "bash zsh fish" -- "$cur") )
                ;;
            logs|lg)
                COMPREPLY=( $(compgen -W "-f -n" -- "$cur") )
                ;;
            warlock|wl)
                COMPREPLY=( $(compgen -W "--no-lan" -- "$cur") )
                ;;
        esac
        return 0
    fi

    if [ "${COMP_CWORD}" -eq 3 ]; then
        case "${COMP_WORDS[1]}/${COMP_WORDS[2]}" in
            config/apply|cfg/apply)
                COMPREPLY=( $(compgen -W "--dry-run --default" -- "$cur") )
                ;;
            models/add|models/a|m/add|m/a)
                COMPREPLY=( $(compgen -W "--link --name" -- "$cur") )
                ;;
            models/delete|m/delete)
                COMPREPLY=( $(compgen -W "--yes" -- "$cur") )
                ;;
        esac
        return 0
    fi

    return 0
}
complete -F _llamawizard llamawizard
`

const zshCompletionScript = `#compdef llamawizard

_llamawizard() {
    local prefix w list
    prefix="${words[CURRENT]}"

    if (( CURRENT == 2 )); then
        list=(status st doctor dr logs lg start stop restart re warlock wl models m config cfg pi completion update up uninstall version v help h --help -h --version -v)
    else
        case "${words[2]:-}" in
            models|m)
                if (( CURRENT == 3 )); then
                    list=(list ls add a show sh remove rm delete)
                else
                    case "${words[2]}/${words[3]:-}" in
                        models/add|models/a|m/add|m/a)
                            list=(--link --name)
                            ;;
                        models/delete|m/delete)
                            list=(--yes)
                            ;;
                    esac
                fi
                ;;
            config|cfg)
                if (( CURRENT == 4 )) && [[ "${words[3]}" == apply ]]; then
                    list=(--dry-run --default)
                else
                    list=(show sh path p apply)
                fi
                ;;
            pi)
                list=(install uninstall)
                ;;
            completion)
                list=(bash zsh fish)
                ;;
            logs|lg)
                list=(-f -n)
                ;;
            warlock|wl)
                list=(--no-lan)
                ;;
        esac
    fi

    reply=()
    for w in $list; do
        [[ $w == $prefix* ]] && reply+=($w)
    done
    return 0
}

compdef _llamawizard llamawizard
`

const fishCompletionScript = `complete -c llamawizard -f -a "status st doctor dr logs lg start stop restart re warlock wl models m config cfg pi completion update up uninstall version v help h --help -h --version -v"
complete -c llamawizard -f -n "__fish_seen_subcommand_from models m" -a "list ls add a show sh remove rm delete"
complete -c llamawizard -f -n "__fish_seen_subcommand_from config cfg" -a "show sh path p apply"
complete -c llamawizard -f -n "__fish_seen_subcommand_from pi" -a "install uninstall"
complete -c llamawizard -f -n "__fish_seen_subcommand_from completion" -a "bash zsh fish"
complete -c llamawizard -f -n "__fish_seen_subcommand_from logs lg" -a "-f -n"
complete -c llamawizard -f -n "__fish_seen_subcommand_from warlock wl" -a "--no-lan"
complete -c llamawizard -f -n "__fish_seen_subcommand_from add a" -a "--link --name"
complete -c llamawizard -f -n "__fish_seen_subcommand_from apply" -a "--dry-run --default"
complete -c llamawizard -f -n "__fish_seen_subcommand_from delete" -a "--yes"
`
