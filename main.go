package main

import "github.com/calvin/gh-stacked/cmd"

var version = "dev"

func main() {
	cmd.Version = version
	cmd.Execute()
}
