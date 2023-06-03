## bbk

bbk is a powerful tool

## build

```shell script
bash build.sh
```



## 功能


1. 支持tcp/tls/http2/websocket作为传输协议
2. aes,bf-cfb,camellia,des,rc2,rc4等加密方式
3. client支持socks5/connect接入方式
4. 多个http请求复用同一隧道(多路复用)
## server

```json
{
    "mode": "server",
    "listenAddr": "127.0.0.1",
    "listenPort": 5900,
    "logLevel": "info",
    "method": "aes-256-cfb",
    "password": "p@ssword",
    "workMode": "tcp",
    "workPath": "/websocket"
}

```

```sh
node bin/bbk.js -c etc/server.json
```

## client

```json
{
    "mode": "client",
    "listenAddr": "0.0.0.0",
    "listenPort": 1090,
    "listenHttpPort": 1087,
    "logLevel": "info",
    "tunnelOpts": {
        "protocol": "ws",
        "secure": false,
        "host": "127.0.0.1",
        "port": "5900",
        "path": "/websocket",
        "method": "aes-256-cfb",
        "password": "p@ssword"
    },
    "ping": true
}

```

```sh
node bin/bbk.js -c etc/client.json
```

## 更多例子参考examples目录
