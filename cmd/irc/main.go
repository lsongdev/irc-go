package main

import (
	"bufio"
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"strings"
	"time"

	"github.com/lsongdev/irc-go/client"
	"github.com/lsongdev/irc-go/protocol"
)

func main() {
	addr := flag.String("server", "localhost:6667", "IRC server address")
	nick := flag.String("nick", "guest", "nickname")
	user := flag.String("user", "", "username (defaults to nick)")
	realname := flag.String("realname", "irc-go user", "real name")
	password := flag.String("password", "", "optional server password")
	channel := flag.String("channel", "", "channel to join after registration")
	flag.Parse()
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	c, err := client.DialContext(ctx, client.Config{Address: *addr, Nick: *nick, Username: *user, RealName: *realname, Password: *password})
	if err != nil {
		fmt.Fprintln(os.Stderr, "connect:", err)
		os.Exit(1)
	}
	defer c.Close()
	active := *channel
	input := make(chan string)
	go func() {
		defer close(input)
		scanner := bufio.NewScanner(os.Stdin)
		for scanner.Scan() {
			input <- scanner.Text()
		}
	}()
	fmt.Println("Commands: /join #channel, /part, /msg nick text, /nick name, /raw ..., /quit")
	ready, messages, errs := c.Ready(), c.Messages(), c.Errors()
	for {
		select {
		case <-ready:
			ready = nil
			fmt.Println("connected as", c.Nick())
			if active != "" {
				if err := c.Join(active); err != nil {
					fmt.Fprintln(os.Stderr, "error:", err)
				}
			}
		case m, ok := <-messages:
			if !ok {
				return
			}
			printMessage(m)
		case err, ok := <-errs:
			if !ok {
				errs = nil
			} else if err != nil {
				fmt.Fprintln(os.Stderr, "connection:", err)
			}
		case line, ok := <-input:
			if !ok {
				_ = c.Quit("Input closed")
				return
			}
			var quit bool
			active, quit, err = handleInput(c, active, line)
			if err != nil {
				fmt.Fprintln(os.Stderr, "error:", err)
			}
			if quit {
				return
			}
		case <-ctx.Done():
			_ = c.Quit("Interrupted")
			return
		}
	}
}

func handleInput(c *client.Client, active, line string) (string, bool, error) {
	if line == "" {
		return active, false, nil
	}
	if !strings.HasPrefix(line, "/") {
		if active == "" {
			return active, false, fmt.Errorf("join a channel or use /msg first")
		}
		return active, false, c.Privmsg(active, line)
	}
	cmd, args, _ := strings.Cut(line[1:], " ")
	switch strings.ToLower(cmd) {
	case "join":
		active = strings.TrimSpace(args)
		return active, false, c.Join(active)
	case "part":
		return active, false, c.Part(active, strings.TrimSpace(args))
	case "msg":
		to, text, ok := strings.Cut(args, " ")
		if !ok {
			return active, false, fmt.Errorf("usage: /msg target text")
		}
		return active, false, c.Privmsg(to, text)
	case "nick":
		return active, false, c.NickChange(strings.TrimSpace(args))
	case "raw":
		return active, false, sendRaw(c, args)
	case "quit":
		return active, true, c.Quit(strings.TrimSpace(args))
	default:
		return active, false, fmt.Errorf("unknown command /%s", cmd)
	}
}

func sendRaw(c *client.Client, line string) error {
	m, err := protocol.ParseMessage(line)
	if err != nil {
		return err
	}
	return c.Send(m)
}

func printMessage(m protocol.Message) {
	now := time.Now().Format("15:04")
	source := ""
	if m.Prefix != nil {
		source = m.Prefix.Name
	}
	switch m.Command {
	case "PRIVMSG":
		if len(m.Params) >= 2 {
			fmt.Printf("[%s] <%s> %s\n", now, source, m.Params[1])
		}
	case "NOTICE":
		if len(m.Params) >= 2 {
			fmt.Printf("[%s] -%s- %s\n", now, source, m.Params[1])
		}
	case "JOIN", "PART", "QUIT", "NICK", "TOPIC", "KICK":
		fmt.Printf("[%s] * %s %s %s\n", now, source, m.Command, strings.Join(m.Params, " "))
	default:
		if len(m.Command) == 3 && m.Command[0] >= '0' && m.Command[0] <= '9' {
			fmt.Printf("[%s] %s\n", now, strings.Join(m.Params[1:], " "))
		}
	}
}
