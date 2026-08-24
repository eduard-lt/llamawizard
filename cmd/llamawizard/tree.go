package main

// command is one node in the CLI command tree: a canonical name, optional
// short-form aliases, and (for nouns) its subcommands.
type command struct {
	Name     string     // canonical name
	Aliases  []string   // short forms (resolve identically to Name)
	Children []*command // subcommands (nouns only)
}

// commandTree is the single source of truth for every command and
// subcommand the CLI accepts. resolve() dispatches against it, and the
// "Did you mean" suggestions are drawn from it.
var commandTree = []*command{
	{Name: "status", Aliases: []string{"st"}},
	{Name: "doctor", Aliases: []string{"dr"}},
	{Name: "logs", Aliases: []string{"lg"}},
	{Name: "start"}, {Name: "stop"},
	{Name: "restart", Aliases: []string{"re"}},
	{Name: "warlock", Aliases: []string{"wl"}},
	{Name: "models", Aliases: []string{"m"}, Children: []*command{
		{Name: "list", Aliases: []string{"ls"}},
		{Name: "add", Aliases: []string{"a"}},
		{Name: "show", Aliases: []string{"sh"}},
		{Name: "remove", Aliases: []string{"rm"}},
		{Name: "delete"}, // destructive: no alias
	}},
	{Name: "config", Aliases: []string{"cfg"}, Children: []*command{
		{Name: "show", Aliases: []string{"sh"}},
		{Name: "path", Aliases: []string{"p"}},
	}},
	{Name: "pi", Children: []*command{
		{Name: "install"}, {Name: "uninstall"},
	}},
	{Name: "update", Aliases: []string{"up"}},
	{Name: "uninstall"},
	{Name: "version", Aliases: []string{"v"}},
	{Name: "help", Aliases: []string{"h"}},
}
