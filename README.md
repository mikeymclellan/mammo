# Mammo - Go Module for Mammotion Lawnmowers

Mammo is a Go module for controlling and monitoring Mammotion robot mowers (Luba, Luba 2 & Yuka) via MQTT cloud Cloud.

This module is a work in progress and is not yet complete. It's heavily based on
the [PyMammotion](https://github.com/mikey0000/PyMammotion) Python project and
ported to Go.

## Test Command

There's a test command that can be used to test the connection to the MQTT
server.

```bash
go run main.go login --username [username] --password [password]
```

## References

Some docs from Aliyun on their [Living Link APIs](https://g.alicdn.com/aic/ilop-docs/1.0.22/index.html)

