package cmd

import "strings"

// logo is "clonager" in figlet's standard font.
var logo = []string{
	`      _`,
	`  ___| | ___  _ __   __ _  __ _  ___ _ __`,
	` / __| |/ _ \| '_ \ / _` + "`" + ` |/ _` + "`" + ` |/ _ \ '__|`,
	`| (__| | (_) | | | | (_| | (_| |  __/ |`,
	` \___|_|\___/|_| |_|\__,_|\__, |\___|_|`,
	`                          |___/`,
}

// banner is the logo with the version below it, shown by `clonager --help`
// and `clonager --version`.
func banner() string {
	return styleBranch.Render(strings.Join(logo, "\n")) + "\n" +
		styleDim.Render("version "+version)
}

func init() {
	rootCmd.Long = banner() + "\n\n" + rootCmd.Short + "\n\n" + statusHelp
	rootCmd.SetVersionTemplate(banner() + "\n")
}
