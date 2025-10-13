// --- START OF NEW FILE test/tls_client.go ---
package main

//go run ./test/tls_client.go 127.0.0.1:8888 www.google.com

// go run ./test/tls_client.go 127.0.0.1:8888 example.com

import (
	"context"
	"crypto/tls"
	"fmt"
	"io"
	"log"
	"net"
	"os"
	"time"
)

func main() {
	if len(os.Args) < 3 {
		fmt.Println("Usage: go run ./test/tls_client.go <proxy_addr> <target_domain>")
		fmt.Println("Example: go run ./test/tls_client.go 127.0.0.1:8888 www.google.com")
		return
	}

	proxyAddr := os.Args[1]
	targetDomain := os.Args[2]
	net.JoinHostPort(targetDomain, "443")

	log.Printf(">>> TLS Client Tester for Transparent Proxy <<<")
	log.Printf("Proxy (Simulator) Address: %s", proxyAddr)
	log.Printf("Target Domain (SNI):       %s", targetDomain)
	log.Println("-------------------------------------------------")

	// 1. 连接到我们的代理/测试器
	log.Printf("[Step 1] Dialing TCP connection to proxy at %s...", proxyAddr)
	conn, err := net.DialTimeout("tcp", proxyAddr, 5*time.Second)
	if err != nil {
		log.Fatalf("!!! FAILED to connect to proxy: %v", err)
	}
	defer conn.Close()
	log.Println("    > OK. TCP connection established.")
	log.Println("    > NOTE: NO 'CONNECT' request will be sent.")

	// 2. 在这个原始TCP连接上发起TLS握手
	log.Printf("[Step 2] Starting TLS handshake over the raw connection to target '%s'...", targetDomain)
	tlsConn := tls.Client(conn, &tls.Config{
		ServerName:         targetDomain, //
		InsecureSkipVerify: true,         // 在测试中我们不关心证书校验
	})

	// 为握手设置超时
	handshakeCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := tlsConn.HandshakeContext(handshakeCtx); err != nil {
		log.Fatalf("!!! FAILED TLS handshake: %v", err)
	}
	defer tlsConn.Close()
	log.Println("    > OK. TLS handshake successful.")

	// 3. 发送一个简单的HTTP GET请求
	log.Printf("[Step 3] Sending HTTP GET request to / ...")
	request := fmt.Sprintf("GET / HTTP/1.1\r\nHost: %s\r\nConnection: close\r\nUser-Agent: Go-TLS-Client-Tester\r\n\r\n", targetDomain)
	if _, err := tlsConn.Write([]byte(request)); err != nil {
		log.Fatalf("!!! FAILED to write HTTP request: %v", err)
	}
	log.Println("    > OK. Request sent.")

	// 4. 读取并打印响应
	log.Println("[Step 4] Reading response from server...")
	response, err := io.ReadAll(tlsConn)
	if err != nil {
		log.Printf("!!! WARN: Failed to read full response, but this might be ok. Error: %v", err)
	}

	fmt.Println("\n--- SERVER RESPONSE (first 500 bytes) ---")
	if len(response) > 500 {
		fmt.Println(string(response[:500]))
		fmt.Println("...")
	} else {
		fmt.Println(string(response))
	}
	fmt.Println("--- END OF RESPONSE ---")

	log.Println("\n+++ TEST COMPLETED SUCCESSFULLY +++")
}

// --- END OF NEW FILE ---
