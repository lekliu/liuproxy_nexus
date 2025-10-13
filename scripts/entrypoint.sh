#!/bin/sh

# 立即退出，如果任何命令失败
set -e

# --- 从环境变量读取配置，并设置默认值 ---

# TCP透明代理开关
TRANSPARENT_PROXY_TCP_ENABLED=${TRANSPARENT_PROXY_TCP_ENABLED:-"true"}
# UDP透明代理开关
TRANSPARENT_PROXY_UDP_ENABLED=${TRANSPARENT_PROXY_UDP_ENABLED:-"true"}

# 其他配置项
TPROXY_PORT=${TPROXY_PORT:-12345}
MSS_CLAMPING_ENABLED=${MSS_CLAMPING_ENABLED:-"true"}
EXCLUDED_IPS=${EXCLUDED_IPS:-"0.0.0.0/8,10.0.0.0/8,127.0.0.0/8,169.254.0.0/16,172.16.0.0/12,192.168.0.0/16,224.0.0.0/4,240.0.0.0/4"}

# 检查是否需要配置任何透明代理规则
if [ "$TRANSPARENT_PROXY_TCP_ENABLED" = "true" ] || [ "$TRANSPARENT_PROXY_UDP_ENABLED" = "true" ]; then
    echo "Transparent proxy is enabled for at least one protocol. Configuring network..."

    # 1. 启用内核IP转发 (只要有一个代理开启，就需要)
    echo "Enabling IP forwarding..."
    sysctl -w net.ipv4.ip_forward=1

    # 2. 按需配置TCP代理
    if [ "$TRANSPARENT_PROXY_TCP_ENABLED" = "true" ]; then
        echo "--> TCP transparent proxy is ENABLED."

        # 2.1 MSS Clamping (仅用于TCP)
        if [ "$MSS_CLAMPING_ENABLED" = "true" ]; then
            echo "    Applying MSS clamping for TCP..."
            iptables -t mangle -A FORWARD -p tcp --tcp-flags SYN,RST SYN -j TCPMSS --clamp-mss-to-pmtu
        fi

        # 2.2 设置TCP REDIRECT规则
        echo "    Setting up TCP REDIRECT rules..."
        iptables -t nat -N LIUPROXY_TPROXY_TCP
        iptables -t nat -F LIUPROXY_TPROXY_TCP

        for ip in $(echo "$EXCLUDED_IPS" | tr ',' ' '); do
            iptables -t nat -A LIUPROXY_TPROXY_TCP -d "${ip}" -j RETURN
        done

        iptables -t nat -A LIUPROXY_TPROXY_TCP -p tcp -j REDIRECT --to-port "${TPROXY_PORT}"
        iptables -t nat -A PREROUTING -p tcp -j LIUPROXY_TPROXY_TCP
        echo "    TCP REDIRECT rules applied."
    else
        echo "--> TCP transparent proxy is DISABLED."
    fi

    # 3. 按需配置UDP代理
    if [ "$TRANSPARENT_PROXY_UDP_ENABLED" = "true" ]; then
        echo "--> UDP transparent proxy is ENABLED."

        # 3.1 设置 UDP DNAT 规则
        echo "    Setting up UDP DNAT rules..."
        iptables -t nat -N LIUPROXY_DNAT_UDP
        iptables -t nat -F LIUPROXY_DNAT_UDP

        for ip in $(echo "$EXCLUDED_IPS" | tr ',' ' '); do
            iptables -t nat -A LIUPROXY_DNAT_UDP -d "${ip}" -j RETURN
        done

        iptables -t nat -A LIUPROXY_DNAT_UDP -p udp -j DNAT --to-destination ":${TPROXY_PORT}"
        iptables -t nat -A PREROUTING -p udp -j LIUPROXY_DNAT_UDP
        echo "    UDP DNAT rules applied."
    else
        echo "--> UDP transparent proxy is DISABLED."
    fi

else
    echo "Transparent proxy is DISABLED for both TCP and UDP. Skipping all network rule configuration."
fi

# 4. 使用 exec 启动主程序
echo "Starting LiuProxy Gateway..."
exec /app/liuproxy-gateway --configdir /app/configs