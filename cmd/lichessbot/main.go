// Command lichessbot runs the champion engine as a lichess bot: it reads
// LICHESS_BOT_TOKEN, accepts standard-chess challenges, and answers every
// move with the champion's own search (PlayerPickWith), never an outside
// engine's evaluation.
//
// Usage:
//
//	LICHESS_BOT_TOKEN=xxx go run ./lichessbot -champion champion.json -username tidymazebot
//
// The username must be the bot account's own lichess handle, lowercase,
// so the bot can tell which side of each game is itself.
package main

import (
	"flag"
	"fmt"
	"log"
	"os"

	"chess/engine"
	"chess/lichessbot"
)

func main() { os.Exit(run(os.Args[1:])) }

func run(args []string) int {
	fs := flag.NewFlagSet("lichessbot", flag.ContinueOnError)
	champion := fs.String("champion", "champion.json", "champion file to play")
	username := fs.String("username", "", "the bot account's own lichess username, lowercase")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if *username == "" {
		fmt.Fprintln(os.Stderr, "lichessbot: -username is required")
		return 2
	}
	token := os.Getenv("LICHESS_BOT_TOKEN")
	if token == "" {
		fmt.Fprintln(os.Stderr, "lichessbot: LICHESS_BOT_TOKEN is not set")
		return 2
	}
	if _, err := os.Stat(*champion); err != nil {
		fmt.Fprintln(os.Stderr, "lichessbot: champion:", err)
		return 1
	}
	p, err := engine.ReadChampion(*champion).PlayerOrError()
	if err != nil {
		fmt.Fprintln(os.Stderr, "lichessbot: champion:", err)
		return 1
	}

	b := &lichessbot.Bot{
		API:      lichessbot.NewAPI(token),
		Player:   p,
		Username: *username,
		Log:      log.New(os.Stdout, "", log.LstdFlags),
	}
	if err := b.Run(); err != nil {
		fmt.Fprintln(os.Stderr, "lichessbot:", err)
		return 1
	}
	return 0
}
