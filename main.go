package main

import (
	"go.steado.tech/dock/internal/cli"
	"go.steado.tech/dock/internal/config"
)

func main() {
	config.Init()
	cli.Execute()
}
