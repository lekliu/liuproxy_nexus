// --- START OF NEW FILE test/simple_server.go ---
package main

import (
	"log"
	"net"
	"time"
)

func handle(conn net.Conn) {
	defer conn.Close()
	log.Printf("!!! CONNECTION ACCEPTED from %s !!!", conn.RemoteAddr())
	conn.SetWriteDeadline(time.Now().Add(5 * time.Second))
	_, err := conn.Write([]byte("Hello! Connection successful.\n"))
	if err != nil {
		log.Printf("Error writing to client: %v", err)
	}
}

func main() {
	port := "9088"
	addr := "0.0.0.0:" + port

	log.Printf(">>> Simple Server: Starting to listen on %s...", addr)

	listener, err := net.Listen("tcp", addr)
	if err != nil {
		log.Fatalf("!!! FAILED to listen on %s: %v", addr, err)
	}

	log.Printf(">>> Simple Server: Successfully listening on %s. Waiting for connections...", listener.Addr())

	for {
		conn, err := listener.Accept()
		if err != nil {
			log.Printf("Accept error: %v", err)
			continue
		}
		go handle(conn)
	}
}
