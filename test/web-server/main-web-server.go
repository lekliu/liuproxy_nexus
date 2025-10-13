// --- START OF REPLACEMENT for liuproxy_go/cmd_test/echo_server/main.go (Greeting Server) ---
package main

import (
	"log"
	"net"
)

// handleConnection 简单地发送一条欢迎消息然后关闭连接。
func handleConnection(conn net.Conn) {
	// 确保连接在函数结束时总是被关闭
	defer conn.Close()

	// 准备要发送的欢迎消息
	greetingMessage := "Hello from the Greeting Server! If you see this, the proxy is working.\r\n"

	// 发送消息
	conn.Write([]byte(greetingMessage))

	// defer conn.Close() 会在这里执行，连接被关闭。
}

func main() {
	port := "9999"
	listener, err := net.Listen("tcp", ":"+port)
	if err != nil {
		log.Fatalf("!!! Failed to start greeting server on port %s: %v", port, err)
	}
	defer listener.Close()

	//log.Printf(">>> Simple TCP Greeting Server is listening on port %s <<<", port)

	for {
		conn, err := listener.Accept()
		if err != nil {
			//log.Printf("!!! Failed to accept connection: %v", err)
			continue
		}
		// 为每个连接启动一个 goroutine
		go handleConnection(conn)
	}
}

// --- END OF REPLACEMENT ---
