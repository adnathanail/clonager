package cmd

import (
	"strings"
	"testing"
)

func TestFolderLink(t *testing.T) {
	cases := map[string]string{
		"vscode": "vscode://file/Users/me/My%20Projects/vip-proj",
		"cursor": "cursor://file/Users/me/My%20Projects/vip-proj",
		"zed":    "zed://file/Users/me/My%20Projects/vip-proj",
		"files":  "file:///Users/me/My%20Projects/vip-proj",
		"none":   "",
	}
	defer func(prev string) { openIn = prev }(openIn)
	for app, want := range cases {
		openIn = app
		got := folderLink("/Users/me/My Projects/vip-proj", "vip-proj")
		switch {
		case want == "":
			if got != "vip-proj" {
				t.Errorf("%s: got %q, want plain text", app, got)
			}
		case !strings.Contains(got, "\x1b]8;;"+want+"\a"):
			t.Errorf("%s: got %q, want a link to %s", app, got, want)
		}
	}
}
