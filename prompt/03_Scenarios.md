# LiuProxy Nexus v4.0 - 应用场景

本文档是 `liuproxy_nexus` 项目的“用户故事集”，详细描述了项目的核心应用场景。它旨在确保开发团队对需求有统一和清晰的理解，并为功能设计、实现和测试提供明确的上下文。

---

## 场景一：家庭/小型办公透明网关

*   **目标**:
    *   将 `liuproxy_nexus` 部署为一个局域网内的中央网关。
    *   为局域网内的**所有**或**特定**设备（如手机、电视、游戏机）提供无感知的、自动化的智能流量分流，无需在这些设备上进行任何手动配置。
    *   实现对设备流量的访问控制（防火墙）。

*   **网络拓扑 (双子网方案)**:
    ```mermaid
    graph LR
        subgraph "外网"
            Internet
        end

        subgraph "主网络 (192.168.0.0/24)"
            R1[主路由<br>GW: 192.168.0.1]
            PC[个人电脑]
        end

        subgraph "工作网络 (192.168.1.0/24)"
            R2[工作路由<br>GW: 192.168.1.1<br>DHCP Server]
            subgraph "Ubuntu 小主机"
                Docker[Docker] --> C[Nexus 容器<br>IP: 192.168.1.2]
            end
            P1[手机 1]
            P2[手机 2]
            PN["... (150台)"]
        end
        
        Internet -- "WAN" --> R1
        R1 -- "LAN" --> PC
        R1 -- "LAN" --> R2
        
        R2 -- "LAN" --> Docker
        R2 -- "LAN" --> P1 & P2 & PN

        subgraph "数据流"
            P1 -- "默认网关" --> C
            C -- "默认网关" --> R2
        end

        style C fill:#f9f,stroke:#333,stroke-width:2px
    ```

*   **关键组件与可行性分析**:
    1.  **部署模式**: 采用 Docker `macvlan` 网络模式，使 Nexus 容器获得独立的局域网 IP (`192.168.1.2`)。
    2.  **流量拦截**: 在工作路由器 (R2) 的 DHCP 服务中，将**默认网关**地址设置为 Nexus 容器的 IP。所有连接到 R2 的设备（手机）的流量都会自动发往 Nexus 容器。
    3.  **核心模块**: `TransparentGateway` 模块通过 `iptables` 规则拦截被重定向的 TCP/UDP 流量，并获取其原始目标地址。
    4.  **分流逻辑**: `Dispatcher` 根据 `settings.json` 中配置的防火墙和路由规则（特别是 `source_ip` 规则）进行决策，实现对每台手机的精细化通道分配。
    5.  **DNS (未来)**: 为实现更可靠的连接和高级路由，工作路由器的 DHCP 也应将 **DNS 服务器**地址指向 Nexus 容器 IP，由容器内置的 DNS 服务进行处理和劫持。

---

## 场景二：个人设备转发代理

*   **目标**:
    *   将 `liuproxy_nexus` 作为个人电脑或服务器上的一个标准代理服务运行。
    *   为需要代理的特定应用程序（如浏览器、开发工具）提供一个 SOCKS5/HTTP 代理端点。

*   **工作流程**:
    1.  用户在操作系统或应用程序的网络设置中，手动将代理服务器设置为 `liuproxy_nexus` 的 IP 和端口（如 `127.0.0.1:9099`）。
    2.  `Gateway` 模块接收到代理请求，嗅探出协议（SOCKS5/HTTP）和目标地址。
    3.  `Dispatcher` 根据路由规则进行决策（与场景一完全相同）。
    4.  `Gateway` 执行决策，将流量转发到指定的策略实例、直接连接或拒绝。

*   **可行性分析**:
    *   这是项目的基本功能，技术上非常成熟。
    *   **优点**: 部署极其简单（普通 Docker 端口映射或直接运行二进制文件即可），无需特殊网络配置或权限。
    *   **缺点**: 需要在每个客户端设备/应用上手动配置；部分不遵循系统代理设置的应用程序无法被代理；标准 HTTP 代理不支持 UDP 流量。

---

## 场景三：动态公共代理池网关

*   **目标**:
    *   解决手动维护大量代理通道繁琐且不可靠的问题。
    *   在 `liuproxy_nexus` 内部**内建**一个代理池管理模块，自动从公共代理网站抓取、验证并动态维护一个高质量的可用代理池。
    *   `liuproxy_nexus` 作为网关，能够智能地使用这个代理池中的资源来转发流量，并在代理失效时自动替换。

*   **系统架构 (内部模块化)**:
    ```mermaid
    graph TD
        subgraph "liuproxy_nexus (单一进程)"
            subgraph "AppServer"
                A[Web Dashboard]
                D[Dispatcher]
                M[ProxyPool Manager]
                
                subgraph "执行通道"
                    Strategy_HTTP[HTTP Proxy Strategy]
                end

                A -- "管理" --> M
                D -- "请求代理" --> M
                Strategy_HTTP -- "上报失败" --> M
            end

            subgraph "ProxyPool Manager (后台 Goroutines)"
                Scraper["网络爬虫"] -- "抓取" --> ProxySites["公共代理网站"]
                Validator["验证器"] -- "测试" --> Scraper
                Scheduler["调度器"] -- "管理" --> Validator
                DB["内存池 &<br>proxies.json"]
                
                Scraper --> DB
                Validator --> DB
                Scheduler -- "触发" --> Validator & Scraper
            end
        end

        UserDevice["用户设备"] --> AppServer
    ```

*   **关键组件与可行性分析**:
    1.  **内部模块 (`proxypool`)**: 代理池的管理功能被实现为一个内建模块，与 `AppServer` 在同一个进程中运行。这简化了部署并消除了组件间的网络通信开销。
    2.  **通道兼容性**: 确认使用现有的 `http` 策略类型即可对接扫描到的公共代理。
    3.  **数据存储**: 采用**文件+内存** (`configs/proxies.json`) 的轻量级方案，无需外部数据库依赖。
    4.  **动态调度**: `proxypool.Manager` 内置一个智能调度器，根据我们制定的**分级间隔策略**，高效地对代理池进行持续的健康检查和生命周期管理。
    5.  **数据同步与替换**: `Dispatcher` 在需要时，直接调用 `Manager` 的 Go 方法获取一个**最优**的可用代理。当代理使用失败时，`Strategy` 通过内部接口通知 `Manager` 对该代理进行**失败降级**，并触发替换逻辑为该通道分配新的可用代理。
---

## 场景四：移动端 VPN 客户端核心 (Android App)

*   **目标**:
    *   将 `liuproxy_nexus` 的 Go 核心编译成一个 `.aar` 库，作为 Android App 的底层网络引擎。
    *   App 利用 Go 核心，为 Android 设备提供一个系统级的 VPN 服务，实现全局流量代理和智能分流。

*   **工作流程 (数据平面)**:
    ```mermaid
    graph TD
        subgraph "Android 手机"
            A["所有 App 的网络流量"] --> B[Android VpnService]
            B -- "捕获 TCP/UDP 包" --> C[TUN 虚拟网卡]
            C -- "IP 包" --> D[hev-socks5-tunnel (tun2socks)]
            D -- "转换为 SOCKS5 请求" --> E["Go 核心 (运行在 127.0.0.1)"]
            
            subgraph "Go 核心 (liuproxy_nexus)"
                E -- "SOCKS5 请求" --> F[Gateway]
                F --> G[Dispatcher]
                G -- "决策" --> H["策略实例 (VLESS, GoRemote, ...)"]
            end

            H -- "加密流量" --> I[手机物理网卡]
        end
        I --> Internet
    ```

*   **关键组件与可行性分析**:
    1.  **Go 核心 (`gomobile bind`)**: 项目的 `mobile/api.go` 文件提供了 App 与 Go 核心交互的桥梁，可行性高。
    2.  **VPN 服务与 tun2socks**: 使用 Android `VpnService` 和 `hev-socks5-tunnel` 实现流量的捕获与协议转换，技术路径成熟。
    3.  **配置与控制**: Android App 负责 UI 和配置管理。启动 VPN 时，将所有激活的服务器配置和路由规则一次性传递给 Go 核心的 `StartVPN` 函数。
    4.  **核心优势 (v4.0 架构)**: App 不再管理单个连接，而是将所有激活的服务器交给 Go 核心。由 Go 核心内部统一的 `Dispatcher` 和健康检查机制负责实时负载均衡和故障转移，极大提升了移动端连接的稳定性和速度。