// file: test/udp_client_manual.go
package main

import (
	"bufio"
	"encoding/binary"
	"fmt"
	"io"
	"log"
	"net"
	"os"
	"time"
)

func main() {
	// ----------------- 配置区 -----------------
	proxyAddr := "127.0.0.1:28456" // 你的 local_server SOCKS5 端口
	targetAddr := "127.0.0.1:4000" // 你的 UDP Echo Server (ncat) 的地址
	// ------------------------------------------

	fmt.Printf("手动SOCKS5 UDP代理测试\n")
	fmt.Printf("代理服务器: %s\n", proxyAddr)
	fmt.Printf("最终UDP目标: %s\n\n", targetAddr)

	// 步骤 1: 建立到SOCKS5代理的TCP控制连接
	fmt.Println("[步骤 1] 正在建立到代理服务器的TCP控制连接...")
	tcpConn, err := net.DialTimeout("tcp", proxyAddr, 10*time.Second)
	if err != nil {
		log.Fatalf("!!! 失败: 无法建立TCP连接. 错误: %v", err)
	}
	defer tcpConn.Close()
	fmt.Println("    > 成功. TCP连接已建立.")

	// 步骤 2: 发送SOCKS5认证请求 (无需认证)
	fmt.Println("[步骤 2] 正在发送SOCKS5认证请求...")
	// VER=5, NMETHODS=1, METHODS=0x00(No Auth)
	authRequest := []byte{0x05, 0x01, 0x00}
	if _, err := tcpConn.Write(authRequest); err != nil {
		log.Fatalf("!!! 失败: 无法发送认证请求. 错误: %v", err)
	}
	fmt.Printf("    > 成功. 已发送: %x\n", authRequest)

	// 步骤 3: 读取SOCKS5认证响应
	fmt.Println("[步骤 3] 正在等待代理服务器的认证响应...")
	authResponse := make([]byte, 2)
	if _, err := io.ReadFull(tcpConn, authResponse); err != nil {
		log.Fatalf("!!! 失败: 无法读取认证响应. 错误: %v", err)
	}
	if authResponse[0] != 0x05 || authResponse[1] != 0x00 {
		log.Fatalf("!!! 失败: 服务器返回了无效的认证响应: %x", authResponse)
	}
	fmt.Printf("    > 成功. 收到响应: %x\n", authResponse)

	// 步骤 4: 发送 UDP ASSOCIATE 命令
	fmt.Println("[步骤 4] 正在发送 UDP ASSOCIATE 命令...")
	// VER=5, CMD=3(UDP), RSV=0, ATYP=1(IPv4), DST.ADDR=0.0.0.0, DST.PORT=0
	udpRequest := []byte{0x05, 0x03, 0x00, 0x01, 0, 0, 0, 0, 0, 0}
	if _, err := tcpConn.Write(udpRequest); err != nil {
		log.Fatalf("!!! 失败: 无法发送 UDP ASSOCIATE 命令. 错误: %v", err)
	}
	fmt.Printf("    > 成功. 已发送: %x\n", udpRequest)

	// 步骤 5: 读取 UDP ASSOCIATE 的响应，获取代理为我们准备的UDP端口
	fmt.Println("[步骤 5] 正在等待 UDP ASSOCIATE 的响应...")
	// 响应格式: VER, REP, RSV, ATYP, BND.ADDR, BND.PORT (至少10字节 for IPv4)
	udpResponse := make([]byte, 10)
	if _, err := io.ReadFull(tcpConn, udpResponse); err != nil {
		log.Fatalf("!!! 失败: 无法读取 UDP ASSOCIATE 的响应. 错误: %v", err)
	}
	if udpResponse[0] != 0x05 || udpResponse[1] != 0x00 {
		log.Fatalf("!!! 失败: UDP ASSOCIATE 请求失败. 响应: %x", udpResponse)
	}

	// 从响应中解析出代理提供的UDP地址和端口
	relayIP := net.IP(udpResponse[4:8]).String()
	relayPort := binary.BigEndian.Uint16(udpResponse[8:10])
	relayAddrStr := fmt.Sprintf("%s:%d", relayIP, relayPort)
	fmt.Printf("    > 成功. 收到响应: %x\n", udpResponse)
	fmt.Printf("    > 代理已在 %s 上为我们准备好UDP端口.\n", relayAddrStr)

	// 步骤 6: 现在，我们可以开始通过那个UDP端口发送数据了
	// 创建一个本地UDP连接，用于向代理的UDP端口发送数据
	relayUDPAddr, err := net.ResolveUDPAddr("udp", relayAddrStr)
	if err != nil {
		log.Fatalf("!!! 失败: 无法解析代理返回的UDP地址. 错误: %v", err)
	}
	udpConn, err := net.DialUDP("udp", nil, relayUDPAddr)
	if err != nil {
		log.Fatalf("!!! 失败: 无法创建到代理UDP端口的连接. 错误: %v", err)
	}
	defer udpConn.Close()
	fmt.Printf("[步骤 6] 成功创建到代理UDP端口 %s 的数据通道.\n", relayAddrStr)

	// ---------------------------------------------------------
	// 代理握手全部完成，现在进入数据收发阶段
	// ---------------------------------------------------------
	fmt.Println("\n---------------------------------------------------------")
	fmt.Println("代理通道已建立。现在可以发送UDP数据了。")
	fmt.Println("按 Ctrl+C 退出。")
	fmt.Println("---------------------------------------------------------")

	// 启动一个 goroutine 用于接收并打印返回的数据
	go func() {
		buf := make([]byte, 2048)
		for {
			n, err := udpConn.Read(buf)
			if err != nil {
				fmt.Printf("\n从代理读取UDP数据时出错: %v\n", err)
				os.Exit(0)
			}
			// SOCKS5返回的UDP包也带有一个头部，我们需要剥离它
			// RSV(2), FRAG(1), ATYP(1), SRC.ADDR, SRC.PORT, DATA
			// 这里为了简单，我们直接打印从第11个字节开始的数据
			if n > 10 {
				fmt.Printf("收到响应: %s", string(buf[10:n]))
			}
		}
	}()

	// 在主 goroutine 中发送数据
	scanner := bufio.NewScanner(os.Stdin)
	for scanner.Scan() {
		text := scanner.Text() + "\n"

		// 构造SOCKS5 UDP数据包
		// 头部: RSV(2), FRAG(1), ATYP(1), DST.ADDR, DST.PORT
		header := []byte{0x00, 0x00, 0x00, 0x01} // RSV, NO-FRAG, IPv4

		// 解析最终目标地址
		destAddr, err := net.ResolveUDPAddr("udp", targetAddr)
		if err != nil {
			continue
		}

		header = append(header, destAddr.IP.To4()...)
		header = append(header, byte(destAddr.Port>>8), byte(destAddr.Port&0xff))

		// 将头部和数据拼接
		packet := append(header, []byte(text)...)

		// 发送
		if _, err := udpConn.Write(packet); err != nil {
			log.Fatalf("发送UDP数据失败: %v", err)
		}
	}
}
