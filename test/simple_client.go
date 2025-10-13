// --- START OF NEW FILE test/simple_client.go ---
package main

import (
	"fmt"
	"io"
	"log"
	"net"
	"time"
)

func main() {
	port := "9088"
	address := "127.0.0.1:" + port

	log.Printf(">>> Simple Client: Attempting to connect to %s...", address)

	conn, err := net.DialTimeout("tcp", address, 5*time.Second)
	if err != nil {
		log.Fatalf("\n--- CONNECTION FAILED! ---\nError: %v\n--------------------------\nThis is a strong indicator that an external factor (like a firewall) is blocking the connection.", err)
	}

	defer conn.Close()
	log.Println("\n+++ CONNECTION SUCCEEDED! +++")

	response, err := io.ReadAll(conn)
	if err != nil {
		log.Printf("Failed to read response: %v", err)
	} else {
		fmt.Printf("Received from server: %s", string(response))
	}
}

// --- END OF NEW FILE ---
