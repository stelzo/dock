package main

import (
	"codeberg.org/stelzo/dock/internal/cli"
	"codeberg.org/stelzo/dock/internal/config"
)

func main() {
	config.Init()
	cli.Execute()
}
