# irc-go

`irc-go` 是一个轻量、零第三方运行时依赖的 IRC 协议库、客户端与服务器。协议编解码、客户端状态机、服务器状态和命令行界面相互独立，可以整包使用，也可以只嵌入需要的部分。

项目遵循 RFC 1459 / RFC 2812 的消息格式、注册流程、数字回复、RFC 1459 大小写映射和每行 512 字节限制，同时解析 IRCv3 message tags。服务器聚焦单节点实时聊天，不把 IRC 的瞬态会话写入数据库。

## 快速开始

要求 Go 1.26 或更高版本。

```sh
go run ./cmd/ircd -addr :6667 -name irc.example -network example
```

在两个终端启动客户端：

```sh
go run ./cmd/irc -server localhost:6667 -nick alice -channel '#general'
go run ./cmd/irc -server localhost:6667 -nick bob -channel '#general'
```

直接输入内容会发送到当前频道。交互客户端支持：

```text
/join #channel
/part optional reason
/msg nick message
/nick newname
/raw WHO #channel
/quit optional reason
```

服务端可用 `-password` 设置连接密码，用 `-motd '第一行|第二行'` 设置多行 MOTD。生产环境建议在 TLS 代理之后运行，或由调用方把 TLS listener 传给 `Server.Serve`。

## 作为 Go 库使用

启动嵌入式服务器：

```go
srv := server.New(server.Config{
    Name:    "irc.example",
    Network: "example",
    Address: ":6667",
    MOTD:    []string{"Welcome"},
})

go func() {
    if err := srv.ListenAndServe(); err != nil {
        log.Fatal(err)
    }
}()

// 退出时调用 srv.Shutdown(ctx)。
```

连接客户端：

```go
c, err := client.Dial(client.Config{
    Address:  "localhost:6667",
    Nick:     "alice",
    Username: "alice",
    RealName: "Alice",
})
if err != nil {
    log.Fatal(err)
}
defer c.Close()

ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
defer cancel()
if err := c.WaitReady(ctx); err != nil {
    log.Fatal(err)
}

_ = c.Join("#general")
_ = c.Privmsg("#general", "hello")
for msg := range c.Messages() {
    fmt.Println(msg.String())
}
```

直接使用协议层：

```go
msg, err := protocol.ParseMessage(":nick!user@host PRIVMSG #go :hello\r\n")
wire, err := msg.Encode() // 始终带 CRLF，并验证 512 字节上限
```

所有连接方法都可并发调用；一个 goroutine 消费 `Messages()` 即可。`Errors()` 用于接收异步传输错误，`Done()` 在连接关闭时结束。

## 协议范围

服务器支持：

- 连接与注册：`CAP`、`PASS`、`NICK`、`USER`、`PING`、`PONG`、`QUIT`
- 消息：`PRIVMSG`、`NOTICE`、away 自动回复
- 频道：`JOIN`、`PART`、`NAMES`、`LIST`、`TOPIC`、`INVITE`、`KICK`
- 查询：`WHO`、`WHOIS`、`MOTD`、`VERSION`
- 频道模式：`i`、`m`、`n`、`t`、`k`、`l`、`o`、`v`

详细的兼容性边界见 [docs/protocol.md](docs/protocol.md)。这是单节点轻量实现，不包含服务器互联、IRC services、历史消息、账号系统、DCC 或持久化频道注册。这些不是实时 IRC 客户端/服务器通信所需的线协议核心，也不会被伪装成已支持能力。

## 开发

```sh
go test -race ./...
go vet ./...
```

目录结构：

```text
protocol/       线协议、数字回复、名称大小写规则
client/         可复用并发客户端 API
server/         可嵌入服务器、连接与各类命令处理
cmd/irc/        终端聊天客户端
cmd/ircd/       服务端进程
docs/           协议与设计文档
```
