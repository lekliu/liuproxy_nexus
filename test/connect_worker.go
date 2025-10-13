// --- START OF NEW FILE test/connect_worker.go ---
package main

import (
	"fmt"
	"net"
	"time"
)

func main() {
	address := "127.0.0.1:8787"
	fmt.Printf(">>> Go TCP Connection Test: Attempting to connect to %s...\n", address)

	// 尝试建立一个纯粹的TCP连接，设置5秒超时
	conn, err := net.DialTimeout("tcp", address, 5*time.Second)
	if err != nil {
		fmt.Printf("\n--- CONNECTION FAILED! ---\n")
		fmt.Printf("Error: %v\n", err)
		fmt.Println("This indicates a problem within the Go runtime's ability to connect,")
		fmt.Println("even though the port is reachable by system tools (like PowerShell).")
		return
	}

	fmt.Printf("\n+++ CONNECTION SUCCEEDED! +++\n")
	fmt.Println("This proves the Go program can establish a basic TCP connection.")
	fmt.Println("The problem is therefore almost certainly in the WebSocket upgrade handshake.")
	conn.Close()
	fmt.Println("Connection closed. Test complete.")
}

// --- END OF NEW FILE ---
