package command

import (
	"fmt"
	"io"

	"github.com/leonfox28/simplus/internal/buildinfo"
)

func RunPlaceholder(name string, args []string, stdout, stderr io.Writer) int {
	if len(args) == 1 && (args[0] == "--version" || args[0] == "version") {
		info := buildinfo.Current()
		fmt.Fprintf(stdout, "%s %s (%s)\n", name, info.Version, info.Commit)
		return 0
	}
	fmt.Fprintf(stderr, "%s runtime is not enabled in the foundation milestone; use --version\n", name)
	return 2
}
