package main

import (
	"fmt"
	"net"
)

func main() {
	backend := "127.0.0.1:63602" // 你的后台代理端口
	conn, err := net.Dial("tcp", backend)
	if err != nil {
		panic(err)
	}
	defer conn.Close()

	req := "GET http://example.com/ HTTP/1.1\r\n" +
		"Host: example.com\r\n" +
		"User-Agent: test\r\n" +
		"Connection: close\r\n" +
		"\r\n"

	conn.Write([]byte(req))

	buf := make([]byte, 4096)
	for {
		n, err := conn.Read(buf)
		if n > 0 {
			fmt.Print(string(buf[:n]))
		}
		if err != nil {
			break
		}
	}
}
