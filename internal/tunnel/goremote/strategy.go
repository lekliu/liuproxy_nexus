package goremote

import (
	"bufio"
	"bytes"
	"context"
	"encoding/binary"
	"fmt"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
	"github.com/xtaci/smux"
	"golang.org/x/net/proxy"
	"io"
	"liuproxy_gateway/internal/shared"
	"liuproxy_gateway/internal/shared/securecrypt"
	"liuproxy_gateway/internal/shared/types"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

const udpGatewaySessionTimeout = 60 * time.Second

// udpGatewaySession 存储了 gateway 端一个 UDP 会话的信息
type udpGatewaySession struct {
	// 与 remote 服务器的 UDP 连接
	remoteConn net.Conn
	// 会话过期时间
	expiry time.Time
}

// GoRemoteStrategy 实现了 TunnelStrategy 接口，采用纯 TCP 短连接模式，并增加 UDP 支持。
type GoRemoteStrategy struct {
	config       *types.Config
	profile      *types.ServerProfile
	logger       zerolog.Logger
	stateManager types.StateManager

	// 用于流量统计
	uplinkBytes   atomic.Uint64
	downlinkBytes atomic.Uint64

	// --- UDP 会话管理 ---
	udpSessions       sync.Map // key: clientIP, value: *udpGatewaySession
	udpSessionCleanup *time.Ticker
	closeOnce         sync.Once
	wg                sync.WaitGroup

	// Mux 会话管理 ---
	session      *smux.Session
	sessionMutex sync.Mutex
}

var _ types.TunnelStrategy = (*GoRemoteStrategy)(nil)

func NewGoRemoteStrategy(cfg *types.Config, profile *types.ServerProfile, stateManager types.StateManager) (types.TunnelStrategy, error) {
	s := &GoRemoteStrategy{
		config:            cfg,
		profile:           profile,
		stateManager:      stateManager,
		udpSessionCleanup: time.NewTicker(30 * time.Second),
		logger: log.With().
			Str("strategy_type", "goremote-v3").
			Str("server_id", profile.ID).
			Str("remarks", profile.Remarks).Logger(),
	}
	s.wg.Add(1)
	go s.cleanupLoop()
	return s, nil
}

// Initialize and InitializeForGateway are no-ops for a stateless strategy.
func (s *GoRemoteStrategy) Initialize() error {
	//s.logger.Debug().Msg("Stateless strategy, Initialize() is a no-op.")
	return nil
}

// InitializeForGateway (重写): 用于处理 SOCKS5 UDP ASSOCIATE
func (s *GoRemoteStrategy) InitializeForGateway() error {
	//s.logger.Debug().Msg("[UDP-Forward] InitializeForGateway called, ready to handle UDP ASSOCIATE.")
	// 对于 goremote，我们不需要在这里预先启动监听器。
	// 当 GetSocksConnection 收到 UDP ASSOCIATE 命令时，会动态创建。
	return nil
}

// GetSocksConnection : 对 UDP ASSOCIATE 的处理
func (s *GoRemoteStrategy) GetSocksConnection() (net.Conn, error) {
	//s.logger.Debug().Msg("[Forward] Creating new in-memory pipe.")
	clientPipe, serverPipe := net.Pipe()

	go s.handleSocksConnection(serverPipe)

	return clientPipe, nil
}

// handleSocksConnection 是 GetSocksConnection 的后台处理器
func (s *GoRemoteStrategy) handleSocksConnection(conn net.Conn) {
	defer conn.Close()

	cmd, targetAddr, err := s.socks5Handshake(conn)
	if err != nil {
		s.logger.Warn().Err(err).Msg("[Forward] SOCKS5 handshake failed.")
		return
	}

	switch cmd {
	case 0x01: // CONNECT (TCP)
		//s.logger.Debug().Str("target", targetAddr).Msg("[Forward-TCP] SOCKS5 handshake successful.")
		s.relayTCP(conn, targetAddr)
	case 0x03: // UDP ASSOCIATE
		//s.logger.Debug().Msg("[Forward-UDP] UDP ASSOCIATE command received.")
		s.handleUDPForward(conn)
	default:
		s.logger.Warn().Uint8("cmd", cmd).Msg("[Forward] Unsupported SOCKS5 command.")
	}
}

// HandleRawTCP (透明代理)
func (s *GoRemoteStrategy) HandleRawTCP(inboundConn net.Conn, targetDest string) {
	//s.logger.Debug().Str("target", targetDest).Msg("[Transparent] Handling raw TCP connection.")
	s.relayTCP(inboundConn, targetDest)
}

// relayTCP (重写): 增加多路复用逻辑分支
func (s *GoRemoteStrategy) relayTCP(inboundConn net.Conn, targetDest string) {
	if s.profile.Multiplex {
		s.relayTCPMux(inboundConn, targetDest)
	} else {
		s.relayTCPMultiConn(inboundConn, targetDest)
	}
}

// dialRemote 是一个内部辅助函数，用于根据配置选择并建立远程连接
func (s *GoRemoteStrategy) dialRemote() (net.Conn, error) {
	//s.logger.Debug().Str("transport", s.profile.Transport).Msg("[GoRemote Dialer] Dialing remote...")

	switch s.profile.Transport {
	case "ws":
		scheme := "ws"
		if s.profile.Scheme == "wss" {
			scheme = "wss"
		}
		host := net.JoinHostPort(s.profile.Address, strconv.Itoa(s.profile.Port))
		u := url.URL{Scheme: scheme, Host: host, Path: s.profile.Path}
		if u.Path == "" {
			u.Path = "/"
		}

		header := http.Header{}
		if s.profile.Host != "" {
			header.Set("Host", s.profile.Host)
		} else {
			header.Set("Host", s.profile.Address)
		}
		header.Set("User-Agent", "liuproxy-goremote-client/3.1")

		return DialWS(context.Background(), u.String(), header)

	case "tcp", "": // 默认或明确指定为 TCP
		remoteAddr := net.JoinHostPort(s.profile.Address, strconv.Itoa(s.profile.Port))
		return DialTCP(remoteAddr)

	default:
		return nil, fmt.Errorf("unsupported transport for goremote: %s", s.profile.Transport)
	}
}

// relayTCP 是 TCP 转发的核心实现，被转发和透明代理共同调用
func (s *GoRemoteStrategy) relayTCPMultiConn(inboundConn net.Conn, targetDest string) {
	countedInbound := shared.NewCountedConn(inboundConn, &s.uplinkBytes, &s.downlinkBytes)
	defer countedInbound.Close()

	//s.logger.Debug().Msg("[Relay-MultiConn] Dialing new remote connection...")

	// 1. 拨号 (使用新的辅助函数)
	remoteConn, err := s.dialRemote()
	if err != nil {
		s.logger.Error().Err(err).Msg("[Relay-MultiConn] Failed to dial remote.")
		if s.stateManager != nil {
			s.stateManager.SetServerStatusDown(s.profile.ID, err.Error())
		}
		return
	}
	defer remoteConn.Close()

	// 2. 发送模式协商字节 (隐式协商，无需发送)

	// 3. 发送加密元数据
	s.sendEncryptedMetadata(remoteConn, targetDest)

	// 4. 双向转发
	s.bidirectionalCopy(countedInbound, remoteConn)
	//s.logger.Debug().Msg("[Relay-MultiConn] Bidirectional copy finished.")
}

// relayTCPMux 是新的多路复用实现
func (s *GoRemoteStrategy) relayTCPMux(inboundConn net.Conn, targetDest string) {
	countedInbound := shared.NewCountedConn(inboundConn, &s.uplinkBytes, &s.downlinkBytes)
	defer countedInbound.Close()

	// 1. 获取或创建 Mux 会话
	session, err := s.getOrCreateMuxSession()
	if err != nil {
		s.logger.Error().Err(err).Msg("[Relay-Mux] Failed to get or create mux session.")
		if s.stateManager != nil {
			s.stateManager.SetServerStatusDown(s.profile.ID, err.Error())
		}
		return
	}

	// 2. 在会话上打开一个新流
	stream, err := session.OpenStream()
	if err != nil {
		s.logger.Error().Err(err).Msg("[Relay-Mux] Failed to open new stream.")
		// 会话可能已损坏，清除它以便下次重建
		s.sessionMutex.Lock()
		if s.session != nil {
			s.session.Close()
			s.session = nil
		}
		s.sessionMutex.Unlock()
		return
	}
	defer stream.Close()
	//s.logger.Debug().Uint32("stream_id", stream.ID()).Msg("[Relay-Mux] New stream opened.")

	// 3. 在流上发送加密元数据
	s.sendEncryptedMetadata(stream, targetDest)

	// 4. 双向转发
	s.bidirectionalCopy(countedInbound, stream)
	//s.logger.Debug().Uint32("stream_id", stream.ID()).Msg("[Relay-Mux] Bidirectional copy finished.")
}

// getOrCreateMuxSession (新增): 管理 Mux 会话的生命周期
func (s *GoRemoteStrategy) getOrCreateMuxSession() (*smux.Session, error) {
	s.sessionMutex.Lock()
	defer s.sessionMutex.Unlock()

	// 如果会话存在且健康，直接返回
	if s.session != nil && !s.session.IsClosed() {
		return s.session, nil
	}

	s.logger.Info().Msg("[Mux] No active session found, creating a new one...")

	// 【关键修改】使用新的拨号辅助函数建立物理连接
	conn, err := s.dialRemote()
	if err != nil {
		return nil, fmt.Errorf("mux physical dial failed: %w", err)
	}

	// 3. 创建 smux 客户端会话
	smuxConfig := smux.DefaultConfig()
	smuxConfig.Version = 2
	smuxConfig.KeepAliveInterval = 10 * time.Second
	smuxConfig.KeepAliveTimeout = 30 * time.Second

	session, err := smux.Client(conn, smuxConfig)
	if err != nil {
		conn.Close()
		return nil, fmt.Errorf("smux client session creation failed: %w", err)
	}

	s.session = session
	//s.logger.Info().Msg("[Mux] New session established successfully.")
	return s.session, nil
}

// sendEncryptedMetadata : 提取出的通用函数，用于发送加密元数据
func (s *GoRemoteStrategy) sendEncryptedMetadata(writer io.Writer, targetDest string) {
	cipher, err := securecrypt.NewCipher(s.config.Crypt)
	if err != nil {
		//s.logger.Error().Err(err).Msg("[Metadata] Failed to create cipher.")
		return
	}

	host, portStr, _ := net.SplitHostPort(targetDest)
	port, _ := strconv.Atoi(portStr)
	meta := &Metadata{
		Type: StreamTCP,
		Addr: host,
		Port: port,
	}

	var metaBuf bytes.Buffer
	if err := WriteMetadata(&metaBuf, meta); err != nil {
		//s.logger.Error().Err(err).Msg("[Metadata] Failed to serialize.")
		return
	}

	encryptedMeta, err := cipher.Encrypt(metaBuf.Bytes())
	if err != nil {
		//s.logger.Error().Err(err).Msg("[Metadata] Failed to encrypt.")
		return
	}

	lenBuf := make([]byte, 2)
	binary.BigEndian.PutUint16(lenBuf, uint16(len(encryptedMeta)))
	if _, err := writer.Write(lenBuf); err != nil {
		//s.logger.Error().Err(err).Msg("[Metadata] Failed to write length header.")
		return
	}
	if _, err := writer.Write(encryptedMeta); err != nil {
		//s.logger.Error().Err(err).Msg("[Metadata] Failed to write payload.")
		return
	}
	//s.logger.Debug().Msg("[Metadata] Encrypted metadata sent.")
}

// bidirectionalCopy (新增): 提取出的通用双向转发函数
func (s *GoRemoteStrategy) bidirectionalCopy(client net.Conn, remote io.ReadWriteCloser) {
	var wg sync.WaitGroup
	wg.Add(2)
	cipher, _ := securecrypt.NewCipher(s.config.Crypt)

	// Uplink (client -> remote)
	go func() {
		defer wg.Done()
		buf := make([]byte, s.config.CommonConf.BufferSize)
		lenBuf := make([]byte, 2)
		for {
			n, err := client.Read(buf)
			if n > 0 {
				encrypted, wErr := cipher.Encrypt(buf[:n])
				if wErr != nil {
					break
				}
				binary.BigEndian.PutUint16(lenBuf, uint16(len(encrypted)))
				if _, wErr = remote.Write(lenBuf); wErr != nil {
					break
				}
				if _, wErr = remote.Write(encrypted); wErr != nil {
					break
				}
			}
			if err != nil {
				if w, ok := remote.(interface{ CloseWrite() error }); ok {
					w.CloseWrite()
				}
				break
			}
		}
	}()

	// Downlink (remote -> client)
	go func() {
		defer wg.Done()
		lenBuf := make([]byte, 2)
		for {
			_, err := io.ReadFull(remote, lenBuf)
			if err != nil {
				break
			}
			payloadLen := binary.BigEndian.Uint16(lenBuf)
			buf := make([]byte, payloadLen)
			_, err = io.ReadFull(remote, buf)
			if err != nil {
				break
			}
			decrypted, rErr := cipher.Decrypt(buf)
			if rErr != nil {
				break
			}
			if _, wErr := client.Write(decrypted); wErr != nil {
				break
			}
		}
		client.Close()
	}()

	wg.Wait()
}

// CloseTunnel : 增加关闭 Mux 会话的逻辑
func (s *GoRemoteStrategy) CloseTunnel() {
	s.closeOnce.Do(func() {
		s.logger.Info().Msg("Closing goremote strategy...")

		s.sessionMutex.Lock()
		if s.session != nil {
			s.logger.Debug().Msg("[Mux] Closing mux session.")
			s.session.Close()
			s.session = nil
		}
		s.sessionMutex.Unlock()

		if s.udpSessionCleanup != nil {
			s.udpSessionCleanup.Stop()
		}
		s.udpSessions.Range(func(key, value interface{}) bool {
			session := value.(*udpGatewaySession)
			session.remoteConn.Close()
			s.udpSessions.Delete(key)
			return true
		})
		s.wg.Wait()
		s.logger.Info().Msg("goremote strategy closed.")
	})
}

// HandleUDPPacket : 处理透明代理的 UDP 包
func (s *GoRemoteStrategy) HandleUDPPacket(packet *types.UDPPacket, sessionKey string) error {
	// 1. 获取或创建到 remote 的 UDP "连接"
	session, err := s.getOrCreateUDPSession(sessionKey, packet.Source)
	if err != nil {
		s.logger.Error().Err(err).Str("client_ip", sessionKey).Msg("[Transparent-UDP] Failed to get or create session.")
		return err
	}

	// 2. 封装 SOCKS5 UDP 包
	var socks5Buf bytes.Buffer
	// RSV, FRAG, ATYP
	socks5Buf.Write([]byte{0x00, 0x00, 0x00})
	dest, ok := packet.Destination.(*net.UDPAddr)
	if !ok {
		return fmt.Errorf("invalid destination address type for UDP")
	}
	if ipv4 := dest.IP.To4(); ipv4 != nil {
		socks5Buf.WriteByte(0x01) // IPv4
		socks5Buf.Write(ipv4)
	} else {
		return fmt.Errorf("IPv6 not yet supported for goremote UDP")
	}
	binary.Write(&socks5Buf, binary.BigEndian, uint16(dest.Port))
	socks5Buf.Write(packet.Payload)

	// 3. 加密并发送
	cipher, _ := securecrypt.NewCipher(s.config.Crypt)
	encryptedPayload, err := cipher.Encrypt(socks5Buf.Bytes())
	if err != nil {
		return err
	}

	_, err = session.remoteConn.Write(encryptedPayload)
	if err != nil {
		s.logger.Warn().Err(err).Msg("[Transparent-UDP] Failed to write to remote.")
		// 连接可能已失效，删除会话，下次将重建
		s.udpSessions.Delete(sessionKey)
		session.remoteConn.Close()
	}
	return err
}

// --- 辅助和接口方法 ---
// socks5Handshake 在给定的连接上执行完整的SOCKS5握手，并返回命令和目标地址
func (s *GoRemoteStrategy) socks5Handshake(conn net.Conn) (cmd byte, targetAddr string, err error) {
	reader := bufio.NewReader(conn)
	// Auth phase
	authHeader := make([]byte, 2)
	if _, err = io.ReadFull(reader, authHeader); err != nil {
		return 0, "", fmt.Errorf("failed to read auth header: %w", err)
	}
	nMethods := int(authHeader[1])
	if _, err = io.CopyN(io.Discard, reader, int64(nMethods)); err != nil {
		return 0, "", fmt.Errorf("failed to discard auth methods: %w", err)
	}
	if _, err = conn.Write([]byte{0x05, 0x00}); err != nil {
		return 0, "", fmt.Errorf("failed to write auth response: %w", err)
	}

	// Request phase
	reqHeader := make([]byte, 4)
	if _, err = io.ReadFull(reader, reqHeader); err != nil {
		return 0, "", fmt.Errorf("failed to read request header: %w", err)
	}

	cmd = reqHeader[1]
	// 我们只处理 CONNECT 和 UDP ASSOCIATE，其他的直接返回
	if cmd != 0x01 && cmd != 0x03 {
		conn.Write([]byte{0x05, 0x07, 0x00, 0x01, 0, 0, 0, 0, 0, 0}) // Command not supported
		return cmd, "", fmt.Errorf("unsupported SOCKS5 command: %d", cmd)
	}

	var host string
	addrType := reqHeader[3]
	switch addrType {
	case 0x01: // IPv4
		addr := make([]byte, 4)
		io.ReadFull(reader, addr)
		host = net.IP(addr).String()
	case 0x03: // Domain
		lenBuf := make([]byte, 1)
		io.ReadFull(reader, lenBuf)
		domain := make([]byte, lenBuf[0])
		io.ReadFull(reader, domain)
		host = string(domain)
	case 0x04: // IPv6
		addr := make([]byte, 16)
		io.ReadFull(reader, addr)
		host = net.IP(addr).String()
	default:
		return cmd, "", fmt.Errorf("unsupported address type: %d", addrType)
	}

	portBuf := make([]byte, 2)
	io.ReadFull(reader, portBuf)
	port := binary.BigEndian.Uint16(portBuf)

	// 对于TCP CONNECT，我们需要回复成功响应
	if cmd == 0x01 {
		if _, err = conn.Write([]byte{0x05, 0x00, 0x00, 0x01, 0, 0, 0, 0, 0, 0}); err != nil {
			return cmd, "", fmt.Errorf("failed to write connect success response: %w", err)
		}
	}
	// UDP ASSOCIATE 的响应将在 handleUDPForward 中单独处理

	return cmd, net.JoinHostPort(host, strconv.Itoa(int(port))), nil
}

func (s *GoRemoteStrategy) GetType() string { return "goremote" }

func (s *GoRemoteStrategy) GetListenerInfo() *types.ListenerInfo {
	// Stateless, no listener
	return nil
}
func (s *GoRemoteStrategy) GetMetrics() *types.Metrics {
	// Short connections, active connections are not easily tracked centrally.
	// This can be improved in the future if needed.
	return &types.Metrics{ActiveConnections: -1}
}

func (s *GoRemoteStrategy) UpdateServer(newProfile *types.ServerProfile) error {
	s.sessionMutex.Lock()
	defer s.sessionMutex.Unlock()

	// 仅更新 profile，不再需要重新创建 dialer
	s.profile = newProfile

	// 如果 multiplex 设置有变，关闭旧的 Mux 会话
	if s.session != nil {
		s.session.Close()
		s.session = nil
	}
	return nil
}

// CheckHealth (旧接口实现): 提供一个轻量级的TCP拨号测试，用于移动端。
func (s *GoRemoteStrategy) CheckHealth() error {
	conn, err := s.dialRemote()
	if err != nil {
		return err
	}
	conn.Close()
	return nil
}

// pipeDialer 实现了 proxy.Dialer 接口，但它的 Dial 方法总是返回预设的 net.Conn
type pipeDialer struct {
	conn net.Conn
}

func (d *pipeDialer) Dial(network, addr string) (net.Conn, error) {
	return d.conn, nil
}

func (s *GoRemoteStrategy) CheckHealthAdvanced() (latency int64, exitIP string, err error) {
	pipeConn, err := s.GetSocksConnection()
	if err != nil {
		return -1, "", fmt.Errorf("goremote CheckHealth: failed to get pipe connection: %w", err)
	}
	defer pipeConn.Close()
	// 注意：这里的 pipeConn 在出错或 transport 关闭时会被自动关闭

	// 创建一个使用我们内存管道的 SOCKS5 拨号器
	dialer, err := proxy.SOCKS5("tcp", "placeholder-unused:1080", nil, &pipeDialer{conn: pipeConn})
	if err != nil {
		pipeConn.Close()
		return -1, "", fmt.Errorf("goremote CheckHealth: failed to create SOCKS5 dialer: %w", err)
	}

	transport := &http.Transport{
		DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
			// DialContext 将忽略传入的 addr，强制使用我们的 SOCKS5 隧道
			return dialer.Dial(network, addr)
		},
	}
	client := &http.Client{
		Transport: transport,
		Timeout:   10 * time.Second,
	}

	const healthCheckURL = "https://www.cloudflare.com/cdn-cgi/trace"
	start := time.Now()
	resp, err := client.Get(healthCheckURL)
	if err != nil {
		return -1, "", fmt.Errorf("goremote CheckHealth: http get failed: %w", err)
	}
	defer resp.Body.Close()

	latencyMs := time.Since(start).Milliseconds()

	if resp.StatusCode != http.StatusOK {
		return latencyMs, "", fmt.Errorf("goremote CheckHealth: unexpected status code: %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return latencyMs, "", fmt.Errorf("goremote CheckHealth: failed to read response body: %w", err)
	}

	scanner := bufio.NewScanner(bytes.NewReader(body))
	for scanner.Scan() {
		line := scanner.Text()
		if strings.HasPrefix(line, "ip=") {
			exitIP = strings.TrimPrefix(line, "ip=")
			break
		}
	}
	return latencyMs, exitIP, nil
}

func (s *GoRemoteStrategy) GetTrafficStats() types.TrafficStats {
	return types.TrafficStats{
		Uplink:   s.uplinkBytes.Load(),
		Downlink: s.downlinkBytes.Load(),
	}
}

// --- UDP 辅助函数 ---

// getOrCreateUDPSession 为透明代理管理 UDP 会话
func (s *GoRemoteStrategy) getOrCreateUDPSession(sessionKey string, clientAddr net.Addr) (*udpGatewaySession, error) {
	if s, ok := s.udpSessions.Load(sessionKey); ok {
		session := s.(*udpGatewaySession)
		session.expiry = time.Now().Add(udpGatewaySessionTimeout)
		return session, nil
	}

	s.logger.Debug().Str("client_ip", sessionKey).Msg("[Transparent-UDP] Creating new session.")
	remoteAddr := net.JoinHostPort(s.profile.Address, strconv.Itoa(s.profile.Port))
	remoteConn, err := net.Dial("udp", remoteAddr)
	if err != nil {
		return nil, err
	}

	newSession := &udpGatewaySession{
		remoteConn: remoteConn,
		expiry:     time.Now().Add(udpGatewaySessionTimeout),
	}
	s.udpSessions.Store(sessionKey, newSession)

	// 为每个会话（即每个客户端）启动一个回复监听 goroutine
	go s.udpTransparentReplyLoop(newSession, clientAddr)

	return newSession, nil
}

// udpTransparentReplyLoop 监听来自 remote 的回复，并将其发回给正确的透明代理客户端
func (s *GoRemoteStrategy) udpTransparentReplyLoop(session *udpGatewaySession, clientAddr net.Addr) {
	buf := make([]byte, s.config.BufferSize)
	cipher, _ := securecrypt.NewCipher(s.config.Crypt)

	// 从 AppServer 获取 UDP 监听器，这是个技巧
	var mainUDPListener net.PacketConn
	if provider, ok := s.stateManager.(interface{ GetUDPListener() net.PacketConn }); ok {
		mainUDPListener = provider.GetUDPListener()
	}

	if mainUDPListener == nil {
		s.logger.Error().Msg("[Transparent-UDP] Could not get main UDP listener from StateManager. Reply loop will fail.")
		return
	}

	for {
		session.remoteConn.SetReadDeadline(time.Now().Add(udpGatewaySessionTimeout + 5*time.Second))
		n, err := session.remoteConn.Read(buf)
		if err != nil {
			s.logger.Debug().Err(err).Str("client_ip", clientAddr.String()).Msg("[Transparent-UDP] Reply loop terminating.")
			session.remoteConn.Close()
			s.udpSessions.Delete(clientAddr.String())
			return
		}

		decrypted, err := cipher.Decrypt(buf[:n])
		if err != nil {
			continue
		}

		_, data, err := parseSocks5UDPHeader(decrypted)
		if err != nil {
			continue
		}

		// 将解包后的数据通过主监听器发回给原始客户端
		_, err = mainUDPListener.WriteTo(data, clientAddr)
		if err != nil {
			s.logger.Warn().Err(err).Str("client_ip", clientAddr.String()).Msg("[Transparent-UDP] Failed to write back to client.")
		}
	}
}

// handleUDPForward 为 SOCKS5 UDP ASSOCIATE 创建转发通道
func (s *GoRemoteStrategy) handleUDPForward(tcpControlConn net.Conn) {
	// 1. 启动一个本地 UDP 监听器
	localListener, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		s.logger.Error().Err(err).Msg("[UDP-Forward] Failed to create local UDP listener.")
		// 发送失败响应
		tcpControlConn.Write([]byte{0x05, 0x01, 0x00, 0x01, 0, 0, 0, 0, 0, 0})
		return
	}
	defer localListener.Close()
	localUDPAddr := localListener.LocalAddr().(*net.UDPAddr)

	// 2. 向 SOCKS5 客户端回复成功，并告知监听地址
	reply := []byte{0x05, 0x00, 0x00, 0x01} // VER, REP, RSV, ATYP(IPv4)
	reply = append(reply, localUDPAddr.IP.To4()...)
	reply = binary.BigEndian.AppendUint16(reply, uint16(localUDPAddr.Port))
	if _, err := tcpControlConn.Write(reply); err != nil {
		s.logger.Warn().Err(err).Msg("[UDP-Forward] Failed to send UDP ASSOCIATE success reply.")
		return
	}
	s.logger.Debug().Str("listen_addr", localUDPAddr.String()).Msg("[UDP-Forward] Local UDP listener created.")

	// 3. 建立到 remote 的 UDP 连接
	remoteAddr := net.JoinHostPort(s.profile.Address, strconv.Itoa(s.profile.Port))
	remoteConn, err := net.Dial("udp", remoteAddr)
	if err != nil {
		s.logger.Error().Err(err).Msg("[UDP-Forward] Failed to dial remote UDP.")
		return
	}
	defer remoteConn.Close()

	// 4. 启动双向转发
	var wg sync.WaitGroup
	wg.Add(2)
	cipher, _ := securecrypt.NewCipher(s.config.Crypt)

	// Uplink (local listener -> remote)
	go func() {
		defer wg.Done()
		buf := make([]byte, s.config.BufferSize)
		for {
			n, clientAddr, err := localListener.ReadFrom(buf)
			if err != nil {
				return
			}
			// buf[:n] 已经是完整的 SOCKS5 UDP 请求包了
			encrypted, _ := cipher.Encrypt(buf[:n])
			if _, err := remoteConn.Write(encrypted); err != nil {
				return
			}
			// 将 clientAddr 存起来，用于下行转发
			s.udpSessions.Store(tcpControlConn.RemoteAddr().String(), clientAddr)
		}
	}()

	// Downlink (remote -> local listener)
	go func() {
		defer wg.Done()
		buf := make([]byte, s.config.BufferSize)
		for {
			n, err := remoteConn.Read(buf)
			if err != nil {
				return
			}
			decrypted, err := cipher.Decrypt(buf[:n])
			if err != nil {
				continue
			}
			// 找到对应的客户端地址并转发
			if val, ok := s.udpSessions.Load(tcpControlConn.RemoteAddr().String()); ok {
				clientAddr := val.(net.Addr)
				if _, err := localListener.WriteTo(decrypted, clientAddr); err != nil {
					// continue
				}
			}
		}
	}()

	// 5. 阻塞，直到TCP控制连接断开
	io.Copy(io.Discard, tcpControlConn)
	s.logger.Debug().Msg("[UDP-Forward] TCP control connection closed, terminating UDP forwarding.")
	// TCP断开后，通过关闭连接来终止两个goroutine
}

func (s *GoRemoteStrategy) cleanupLoop() {
	defer s.wg.Done()
	for {
		select {
		case <-s.udpSessionCleanup.C:
			now := time.Now()
			s.udpSessions.Range(func(key, value interface{}) bool {
				session := value.(*udpGatewaySession)
				if now.After(session.expiry) {
					s.logger.Debug().Str("client_ip", key.(string)).Msg("[UDP] Cleaning up expired session.")
					session.remoteConn.Close()
					s.udpSessions.Delete(key)
				}
				return true
			})
		case <-s.closedChan():
			return
		}
	}
}

func (s *GoRemoteStrategy) closedChan() <-chan struct{} {
	ch := make(chan struct{})
	s.closeOnce.Do(func() {
		close(ch)
	})
	return ch
}

// parseSocks5UDPHeader (辅助函数)
func parseSocks5UDPHeader(data []byte) (*net.UDPAddr, []byte, error) {
	if len(data) < 4 {
		return nil, nil, io.ErrShortBuffer
	}
	offset := 3 // Skip RSV and FRAG
	addrType := data[offset]
	offset++
	var host string
	switch addrType {
	case 0x01: // IPv4
		if len(data) < offset+4+2 {
			return nil, nil, io.ErrShortBuffer
		}
		host = net.IP(data[offset : offset+4]).String()
		offset += 4
	default:
		return nil, nil, fmt.Errorf("unsupported address type in SOCKS5 UDP header: %d", addrType)
	}
	port := binary.BigEndian.Uint16(data[offset : offset+2])
	offset += 2
	addr, err := net.ResolveUDPAddr("udp", net.JoinHostPort(host, strconv.Itoa(int(port))))
	if err != nil {
		return nil, nil, err
	}
	return addr, data[offset:], nil
}
