//go:build linux

package tproxy

import (
	"fmt"
	"net"
)

// GetOriginalDstForUDP 从一个被 TPROXY 的 UDP "连接" (实际上是 socket fd) 中获取其原始目标地址
// 注意：这需要在收到第一个包之后，在该 listener 的 fd 上下文中调用。
// 这个实现比较 tricky，我们暂时使用一个简化的、依赖内核版本的方法。
// 更通用的方法是使用 recvmsg 和 IP_RECVORIGDSTADDR，但那需要重构监听循环。
// 我们暂时假设iptables TPROXY的目标和程序监听的端口是一致的。
func GetOriginalDstForUDP(clientAddr net.Addr, listenPort int) (net.Addr, error) {
	// 对于UDP TPROXY，`getsockopt(SO_ORIGINAL_DST)` 不再有效。
	// 流量被重定向到我们监听的端口，但数据包的目标IP没有被修改。
	// 然而，我们无法轻易地从一个 `net.PacketConn` 的 `ReadFrom` 中获得包的目标IP。

	// 一个常见的、但有局限性的假设是：我们不知道原始目标IP，但我们可以假设
	// 原始目标端口和我们监听的端口是相同的，如果iptables规则是 `--on-port`。
	// `ip rule` 会将包路由到 `lo`，但目标IP保持不变。

	// 这是一个简化实现，它依赖于我们无法直接获取的信息。
	// 在实践中，正确的方法是使用 `syscall.Recvmsg` 配合 `IP_PKTINFO` or `IPV6_PKTINFO`
	// 来获取每个数据包的目标地址。这需要重构 `acceptUDPLoop`。

	// 作为一个折中和简化的开始，我们假设我们能通过某种方式获取目标IP。
	// 现实是，我们不能。`GetOriginalDstForUDP`这个名字是有误导性的。
	// 我们应该直接在`handleUDPPacket`中构造它。

	// 在这个阶段，我们无法可靠地获取原始目标IP。这是一个已知的设计挑战。
	// 我们将返回一个错误，并需要在 `handleUDPPacket` 中改变逻辑。
	return nil, fmt.Errorf("getting original destination for UDP TPROXY with this listener setup is non-trivial; requires recvmsg")
}
