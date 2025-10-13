package main

import (
	"fmt"
	"net"
	"os"
	"time"
)

// 这个程序是用来诊断基础TCP连接问题的。
// 它不包含任何SOCKS5或代理逻辑。
func main() {
	// 从命令行获取要连接的端口号，例如 "9088"
	if len(os.Args) < 2 {
		fmt.Println("用法: go run test_connect.go <端口号>")
		return
	}
	port := os.Args[1]
	address := "127.0.0.1:" + port

	fmt.Printf(">>> 终极测试: 正在尝试建立一个最基础的TCP连接到 %s ...\n", address)

	// 尝试连接，设置5秒超时
	conn, err := net.DialTimeout("tcp", address, 5*time.Second)

	if err != nil {
		// 如果连接失败，打印错误
		fmt.Printf("\n--- 连接失败! ---\n")
		fmt.Printf("错误信息: %v\n", err)
		fmt.Printf("这几乎可以肯定是以下原因之一:\n")
		fmt.Printf("  1. 你的 local_server 程序没有运行，或者没有在监听 %s 端口。\n", address)
		fmt.Printf("  2. 防火墙 (Windows Defender等) 阻止了此连接。\n")
		fmt.Printf("  3. 你运行的 local_server 是旧版本，监听的端口不正确。\n")
		return
	}

	// 如果连接成功
	fmt.Printf("\n+++ 连接成功! +++\n")
	fmt.Printf("Local server 端的控制台现在应该已经打印出了 '!!!!!!!!!! CONNECTION ACCEPTED !!!!!!!!!!' 日志。\n")
	conn.Close()
	fmt.Println("连接已关闭。测试完成。")
}
