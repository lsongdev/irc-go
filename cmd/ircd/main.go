package main

import (
	"context"
	"flag"
	"log/slog"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/lsongdev/irc-go/server"
)

func main() {
	addr := flag.String("addr", ":6667", "TCP address to listen on")
	name := flag.String("name", "irc.local", "IRC server name")
	network := flag.String("network", "irc-go", "IRC network name")
	password := flag.String("password", "", "optional server password")
	motd := flag.String("motd", "Welcome to irc-go", "MOTD lines separated by |")
	verbose := flag.Bool("v", false, "enable debug logging")
	flag.Parse()
	level := slog.LevelInfo
	if *verbose {
		level = slog.LevelDebug
	}
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: level}))
	s := server.New(server.Config{Name: *name, Network: *network, Address: *addr, Password: *password, MOTD: strings.Split(*motd, "|"), Logger: logger})
	sigCtx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	go func() {
		<-sigCtx.Done()
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = s.Shutdown(ctx)
	}()
	if err := s.ListenAndServe(); err != nil {
		logger.Error("server stopped", "error", err)
		os.Exit(1)
	}
}
