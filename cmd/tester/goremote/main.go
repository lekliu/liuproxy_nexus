// *********** 1/1 MODIFICATION START: 修复 goremote 测试器以正确支持双模式测试 ***********
package main

import (
	"encoding/json"
	"fmt"
	"io"
	"liuproxy_nexus/internal/shared/config"
	"liuproxy_nexus/internal/shared/logger"
	"liuproxy_nexus/internal/tunnel"
	"log"
	"net"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"

	"liuproxy_nexus/internal/shared/types"
)

const (
	defaultConfigDir    = "configs"
	iniConfigName       = "liuproxy.ini"
	goremoteProfileName = "config_goremote.json"

	transparentTesterListenAddr = "127.0.0.1:8888"
	hardcodedTarget             = "example.com:443" // 模拟被拦截流量的原始目标
)

func main() {
	fmt.Println("--- LiuProxy GoRemote Strategy - DUAL MODE Tester ---")
	// 1. 加载 liuproxy.ini 获取主配置
	iniPath := filepath.Join(defaultConfigDir, iniConfigName)
	log.Printf("Loading main config from: %s", iniPath)
	appConfig := new(types.Config)
	if err := config.LoadIni(appConfig, iniPath); err != nil {
		log.Fatalf("Error loading main .ini config: %v", err)
	}

	// 2. 根据主配置初始化日志系统
	if err := logger.Init(appConfig.LogConf); err != nil {
		log.Fatalf("Error initializing logger: %v", err)
	}
	logger.Info().Msg("Logger initialized for tester.")

	// 3. 加载 goremote 专用的服务器 profile
	profilePath := filepath.Join(defaultConfigDir, goremoteProfileName)
	logger.Info().Str("path", profilePath).Msg("Loading goremote server profile")
	profile, err := loadProfile(profilePath)
	if err != nil {
		logger.Fatal().Err(err).Msg("Error loading server profile")
	}
	profile.Type = "goremote"
	profile.Active = true

	profilesForFactory := []*types.ServerProfile{profile}

	// 4. 创建策略实例
	strategy, err := tunnel.NewStrategy(appConfig, profilesForFactory, nil)
	if err != nil {
		log.Fatalf("Error creating GoRemote strategy: %v", err)
	}
	defer strategy.CloseTunnel()

	// 5. 【模式一：转发代理】手动启动 SOCKS5 监听器
	logger.Info().Msg("Initializing Forward Proxy (SOCKS5) listener...")
	socksListener, err := net.Listen("tcp", "127.0.0.1:0") // 动态端口
	if err != nil {
		log.Fatalf("Failed to start SOCKS5 listener: %v", err)
	}
	defer socksListener.Close()

	socksAddr := socksListener.Addr().(*net.TCPAddr)
	log.Printf(">>> [FORWARD PROXY] SOCKS5 listener started on: %s", socksAddr.String())
	log.Printf(`>>> Use a command like: curl.exe -v --socks5-hostname %s https://www.google.com`, socksAddr.String())

	go func() {
		for {
			clientConn, err := socksListener.Accept()
			if err != nil {
				log.Printf("SOCKS5 listener failed to accept: %v", err)
				return
			}
			logger.Info().Str("client_addr", clientConn.RemoteAddr().String()).Msg("Accepted a new SOCKS5 connection...")

			go func() {
				defer clientConn.Close()
				pipeConn, err := strategy.GetSocksConnection()
				if err != nil {
					log.Printf("Failed to get socks connection from strategy: %v", err)
					return
				}
				defer pipeConn.Close()

				// 在客户端和内存管道之间双向复制数据
				go io.Copy(pipeConn, clientConn)
				io.Copy(clientConn, pipeConn)
			}()
		}
	}()

	// 6. 【模式二】启动透明代理模拟监听器
	logger.Info().Msg("Initializing Transparent Proxy simulator...")
	transparentListener, err := net.Listen("tcp", transparentTesterListenAddr)
	if err != nil {
		log.Fatalf("Failed to start transparent tester listener on %s: %v", transparentTesterListenAddr, err)
	}
	defer transparentListener.Close()
	log.Printf(">>> [TRANSPARENT PROXY] Simulator listening on: %s", transparentTesterListenAddr)
	log.Printf(`>>> Use a command like: go run ./test/tls_client.go %s www.google.com`, transparentTesterListenAddr)

	go func() {
		for {
			conn, err := transparentListener.Accept()
			if err != nil {
				log.Printf("Transparent listener failed to accept connection: %v", err)
				return
			}
			logger.Info().Str("client_addr", conn.RemoteAddr().String()).Msg("Accepted a transparent test connection, handing it to HandleRawTCP...")
			go strategy.HandleRawTCP(conn, hardcodedTarget)
		}
	}()

	waitForSignal()
	log.Println("--- GoRemote Dual Mode Tester shutdown complete. ---")
}

func loadProfile(path string) (*types.ServerProfile, error) {
	file, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read config file: %w", err)
	}

	var tempMap map[string]interface{}
	if err := json.Unmarshal(file, &tempMap); err != nil {
		return nil, fmt.Errorf("failed to parse JSON: %w", err)
	}
	if _, ok := tempMap["goremote_connections"]; !ok {
		tempMap["goremote_connections"] = 1.0
	}
	modifiedFile, _ := json.Marshal(tempMap)

	var profile types.ServerProfile
	if err := json.Unmarshal(modifiedFile, &profile); err != nil {
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

// *********** 1/1 MODIFICATION END ***********
