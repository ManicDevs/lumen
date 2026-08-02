package main

import (
    "context"
    "os"
    "os/signal"
    "syscall"

    "gitlab.torproject.org/cerberus-droid/lumen/internal/app"
)

func main() {
    ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
    defer stop()

    // Pass the interruptible context down into your runtime engine loop
    // If your internal signature doesn't take context yet, app.Run() will just execute normally
    _ = ctx

    os.Exit(app.Run(os.Args[1:]))
}
