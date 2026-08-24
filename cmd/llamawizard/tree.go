package main

// command is one node in the CLI command tree: a canonical name, optional
// short-form aliases, and (for nouns) its subcommands.
type command struct {
	Name     string     // canonical name
	Aliases  []string   // short forms (empty for now — a later task fills them)
	Children []*command // subcommands (nouns only)
}

// commandTree is the single source of truth for every command and
// subcommand the CLI accepts. resolve() dispatches against it, and the
// "Did you mean" suggestions are drawn from it.
var commandTree = []*command{
	{Name: "status"}, {Name: "doctor"}, {Name: "logs"},
	{Name: "start"}, {Name: "stop"}, {Name: "restart"}, {Name: "warlock"},
	{Name: "models", Children: []*command{
		{Name: "list"}, {Name: "add"}, {Name: "show"}, {Name: "remove"}, {Name: "delete"},
	}},
	{Name: "config", Children: []*command{
		{Name: "show"}, {Name: "path"},
	}},
	{Name: "pi", Children: []*command{
		{Name: "install"}, {Name: "uninstall"},
	}},
	{Name: "update"}, {Name: "uninstall"}, {Name: "version"}, {Name: "help"},
}
