package main

import (
	"os"

	"gitlab.torproject.org/cerberus-droid/lumen/internal/app"
)

func main() {
	os.Exit(app.Run(os.Args[1:]))
}
