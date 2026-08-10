package client

import (
	"fmt"

	"github.com/lsongdev/irc-go/protocol"
)

func (c *Client) Join(channel string, keys ...string) error {
	if !protocol.IsChannel(channel) {
		return fmt.Errorf("irc client: invalid channel %q", channel)
	}
	params := []string{channel}
	if len(keys) > 0 && keys[0] != "" {
		params = append(params, keys[0])
	}
	return c.Send(protocol.Message{Command: "JOIN", Params: params})
}
func (c *Client) Part(channel, reason string) error {
	if !protocol.IsChannel(channel) {
		return fmt.Errorf("irc client: invalid channel %q", channel)
	}
	params := []string{channel}
	if reason != "" {
		params = append(params, reason)
	}
	return c.Send(protocol.Message{Command: "PART", Params: params})
}
func (c *Client) Privmsg(target, text string) error {
	if target == "" || text == "" {
		return fmt.Errorf("irc client: PRIVMSG requires target and text")
	}
	return c.Send(protocol.Message{Command: "PRIVMSG", Params: []string{target, text}})
}
func (c *Client) Notice(target, text string) error {
	if target == "" || text == "" {
		return fmt.Errorf("irc client: NOTICE requires target and text")
	}
	return c.Send(protocol.Message{Command: "NOTICE", Params: []string{target, text}})
}
func (c *Client) NickChange(nick string) error {
	if !protocol.IsNickname(nick) {
		return fmt.Errorf("irc client: invalid nickname %q", nick)
	}
	return c.Send(protocol.Message{Command: "NICK", Params: []string{nick}})
}
func (c *Client) Topic(channel, topic string) error {
	if !protocol.IsChannel(channel) {
		return fmt.Errorf("irc client: invalid channel %q", channel)
	}
	return c.Send(protocol.Message{Command: "TOPIC", Params: []string{channel, topic}})
}
func (c *Client) Mode(target, modes string, args ...string) error {
	return c.Send(protocol.Message{Command: "MODE", Params: append([]string{target, modes}, args...)})
}
func (c *Client) Raw(command string, params ...string) error {
	return c.Send(protocol.Message{Command: command, Params: params})
}
func (c *Client) Quit(reason string) error {
	if reason == "" {
		reason = "Client exiting"
	}
	err := c.Send(protocol.Message{Command: "QUIT", Params: []string{reason}})
	_ = c.Close()
	return err
}
