package main

import (
	"os"
	"path/filepath"

	"github.com/alecthomas/kong"
)

type CLI struct {
	Run    RunCmd    `cmd:"" help:"Benchmark a single query template with many concurrent clients."`
	Folder FolderCmd `cmd:"" help:"Benchmark every query file in a folder."`
	Single SingleCmd `cmd:"" help:"Perform a single search and print the response."`
	Render RenderCmd `cmd:"" help:"Render a query template to JSON without sending it."`
}

func main() {
	var cli CLI

	wd, _ := os.Getwd()
	configPath := filepath.Join(wd, "config.json")

	ctx := kong.Parse(
		&cli,
		kong.Name("bench"),
		kong.Description("Send benchmark traffic at a search node."),
		kong.UsageOnError(),
		kong.Configuration(kong.JSON, configPath),
	)

	ctx.FatalIfErrorf(ctx.Validate())
	ctx.FatalIfErrorf(ctx.Run())
}
