package main

import "github.com/ArchAstro/gh-stacked/cmd"

var version = "dev"

func main() {
	cmd.Version = version
	cmd.Execute()
}
