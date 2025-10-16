// --- START OF NEW FILE cmd/tester/vless/main.go ---
package main

import (
	"encoding/json"
	"fmt"
	"liuproxy_nexus/internal/tunnel"
	"log"
	"os"
	"os/signal"
	"syscall"

	"liuproxy_nexus/internal/shared/types"
)

const defaultConfigPath = "configs/config2_vless.json"

func main() {
	fmt.Println("--- LiuProxy VLESS Strategy Tester ---")
	log.Printf("Loading config from: %s", defaultConfigPath)

	profile, err := loadProfile(defaultConfigPath)
	if err != nil {
		log.Fatalf("Error loading server profile: %v", err)
	}
	profile.Type = "vless"

	appConfig := &types.Config{
		CommonConf: types.CommonConf{BufferSize: 4096},
	}

	profile.Active = true
	profilesForFactory := []*types.ServerProfile{profile}

	activeStrategy, err := tunnel.NewStrategy(appConfig, profilesForFactory)
	if err != nil {
		log.Fatalf("Error creating VLESS strategy: %v", err)
	}

	if err := activeStrategy.Initialize(); err != nil {
		log.Fatalf("Error starting VLESS strategy: %v", err)
	}
	defer activeStrategy.CloseTunnel()

	waitForSignal()
	log.Println("--- VLESS Strategy Tester shutdown complete. ---")
}

func loadProfile(path string) (*types.ServerProfile, error) {
	file, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read config file: %w", err)
	}
	var profile types.ServerProfile
	if err := json.Unmarshal(file, &profile); err != nil {
		return nil, fmt.Errorf("failed to parse JSON config: %w", err)
	}
	return &profile, nil
}

func waitForSignal() {
	sigs := make(chan os.Signal, 1)
	done := make(chan bool, 1)
	signal.Notify(sigs, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-sigs
		fmt.Println()
		log.Println("Signal received, shutting down...")
		done <- true
	}()
	log.Println("Strategy is running. Press Ctrl+C to exit.")
	<-done
}

// --- END OF NEW FILE ---
