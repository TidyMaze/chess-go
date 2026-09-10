// Command uci runs the champion as a UCI engine on stdin and stdout, so a
// build of this engine can be raced against another build, or against
// anything else that speaks UCI.
package main

import (
	"flag"
	"fmt"
	"io"
	"os"

	"chess/engine"
)

func main() { os.Exit(run(os.Args[1:], os.Stdin, os.Stdout, os.Stderr)) }

func run(args []string, in io.Reader, out, errw io.Writer) int {
	fs := flag.NewFlagSet("uci", flag.ContinueOnError)
	fs.SetOutput(errw)
	champion := fs.String("champion", "champion.json", "champion file to play")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	// ReadChampion falls back to a default player when the file is missing,
	// which is the wrong thing for an engine that is about to be raced.
	if _, err := os.Stat(*champion); err != nil {
		fmt.Fprintln(errw, "champion:", err)
		return 1
	}
	p, err := engine.ReadChampion(*champion).PlayerOrError()
	if err != nil {
		fmt.Fprintln(errw, "champion:", err)
		return 1
	}
	engine.ServeUCI(in, out, p)
	return 0
}
