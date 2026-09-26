package rewrite

// Defaults are the built-in rules for harnesses known to be fingerprinted.
// Each edit targets stable boilerplate and preserves meaning. To add a harness:
// capture its request, find which three phrases trip the gate (drop lines until
// it passes), and add two of them here.
var Defaults = []Harness{
	{
		Name: "pi",
		Rules: []Rule{
			{From: "pi itself", To: "the cli itself"},
			{From: "pi packages", To: "cli packages"},
			{From: "pi .md files", To: "cli .md files"},
		},
	},
	{
		Name: "opencode",
		Rules: []Rule{
			{From: "Here is some useful information about the environment you are running in:", To: "Environment information:"},
			{From: "Workspace root folder:", To: "Workspace root:"},
		},
	},
}
