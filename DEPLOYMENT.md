// FILE: DEPLOYMENT.md
# LiuProxy Gateway - 部署与使用指南

本文档提供了将 `LiuProxy Gateway` 部署为独立服务的详细步骤。系统支持两种主要的部署方式：

1.  **网关模式 (Gateway Mode) - 推荐**:
    *   **部署方式**: Docker (使用 `macvlan` 网络)
    *   **功能**: 提供**透明代理**和**防火墙**功能，可作为整个局域网的网关。同时保留了标准的SOCKS5/HTTP转发代理功能。
    *   **适用场景**: 家庭、办公室或任何需要对局域网设备进行集中流量管理和过滤的场景。

2.  **转发代理模式 (Forward Proxy Mode)**:
    *   **部署方式**: Docker (使用端口映射) 或直接运行二进制文件。
    *   **功能**: 仅提供标准的SOCKS5/HTTP转发代理服务。
    *   **适用场景**: 个人PC或服务器上，为特定应用程序提供代理服务。

---

## 1. 网关模式部署 (Docker + macvlan) - 推荐

此模式将 `LiuProxy` 容器变成一个独立的网络设备，拥有自己的局域网IP地址。这使得它可以作为其他设备的网关，实现透明代理。

### 1.1. 前提条件

*   一台 **Linux** 主机 (物理机或虚拟机，例如 Ubuntu, Debian, CentOS)。
*   已安装 [Docker](https://docs.docker.com/engine/install/)。
*   已安装 [Docker Compose](https://docs.docker.com/compose/install/)。

### 1.2. 部署步骤

#### **步骤 1: 准备部署文件**

在你的 Linux 主机上，创建一个部署目录，并组织好文件结构：

```
/opt/liuproxy/
├── docker-compose.yml
└── configs/
    ├── liuproxy.ini
    ├── servers.json
    └── settings.json
```

1.  **`docker-compose.yml`**: 复制项目根目录下的 `docker-compose.yml` 文件到 `/opt/liuproxy/`。
2.  **`configs/` 目录**: 将你开发环境中的 `configs` 目录完整复制到 `/opt/liuproxy/`。

#### **步骤 2: 创建 `macvlan` Docker 网络 (只需一次)**

你需要告诉 Docker 创建一个特殊的网络，让容器可以直接连接到你的局域网。

1.  **查找你的网络信息**:
    *   **物理网卡**: 运行 `ip a`，找到连接到你路由器的网卡名，通常是 `eth0`, `eno1`, `ens18` 等。
    *   **子网和网关**: 运行 `ip route | grep default`，找到你的局域网子网（如 `192.168.0.0/24`）和主路由器IP（如 `192.168.0.1`）。

2.  **执行创建命令**:
    运行以下命令，并**确保替换为你自己的网络信息**。
    ```bash
    # docker network create -d macvlan \
      --subnet=<你的子网> \
      --gateway=<你的主路由器IP> \
      -o parent=<你的物理网卡名> \
      <给这个网络起一个名字>

    # 示例:
    docker network create -d macvlan \
      --subnet=192.168.0.0/24 \
      --gateway=192.168.0.1 \
      -o parent=eno1 \
      maclan_net
    ```
    *   这里的 `maclan_net` 就是我们网络的“真实名称”。

#### **步骤 3: 配置 `docker-compose.yml`**

打开 `/opt/liuproxy/docker-compose.yml` 文件，根据你的网络环境进行**两处必要修改**。

```yaml
version: '3.9'

services:
  gateway:
    image: liuproxy-gateway:latest
    # ... (其他部分保持不变) ...
    networks:
      app_net:
        # 【修改 1】为容器指定一个唯一的、未被占用的局域网IP
        ipv4_address: 192.168.0.101

networks:
  app_net:
    external: true
    # 【修改 2】将 'maclan' 替换为你在上一步创建的网络名
    name: maclan_net
```
*   **`ipv4_address`**: 必须是你的局域网中一个**空闲的IP地址**。建议在路由器上为这个IP地址保留，避免冲突。
*   **`name`**: 必须与你 `docker network create` 时使用的网络名完全一致。

#### **步骤 4: 启动服务**

在 `/opt/liuproxy/` 目录下，执行以下命令：

```bash
docker-compose up -d
```
服务启动后，`LiuProxy` 容器现在就以你指定的IP（例如 `192.168.0.101`）在你的局域网中运行了。

### 1.3. 使用与验证

#### **配置局域网设备**
要让设备通过 `LiuProxy` 上网，你有两种选择：

1.  **修改主路由器 (推荐)**:
    *   登录你的主路由器管理界面。
    *   找到 DHCP 服务器设置。
    *   将 **默认网关 (Default Gateway)** 的地址修改为 `LiuProxy` 容器的IP (例如 `192.168.0.101`)。
    *   保存设置并重启路由器或让设备重新获取IP。
    *   现在，局域网内所有设备的流量都会默认通过 `LiuProxy`。

2.  **手动修改单个设备**:
    *   在你的手机或电脑的网络设置中，将 **网关 (Gateway)** 手动设置为 `LiuProxy` 容器的IP。
    *   这种方式只影响当前设备。

#### **访问 Web UI**
*   在**任何一台**与 `LiuProxy` 容器在**同一个局域网**的设备上（**但不能是运行Docker的宿主机本身**），打开浏览器。
*   访问 `http://<容器的IP地址>:8082` (例如 `http://192.168.0.101:8082`)。
*   你可以在 "Monitor" 页面看到实时流量，在 "Firewall" 和 "Routing" 页面配置规则。

> **重要提示: `macvlan` 的网络限制**
> 出于安全和网络协议栈的设计，**运行Docker的宿主机无法直接通过 `macvlan` IP 访问容器**。例如，在IP为 `192.168.0.100` 的宿主机上，你无法 `ping` 或访问 `192.168.0.101`。请始终使用局域网内的**其他设备**（如你的手机或另一台电脑）来管理和测试。

---

## 2. 转发代理模式部署

此模式更简单，但功能受限，无法作为透明网关。

### 2.1. Docker 部署 (端口映射)

使用 `docker-compose.yml` 的一个简化版本，将容器端口映射到主机。

```yaml
version: '3.9'

services:
  gateway:
    image: liuproxy-gateway:latest
    container_name: liuproxy_gateway_service
    build: .
    volumes:
      - ./configs:/app/configs
    ports:
      # 将容器的 8082 端口映射到主机的 8082 端口
      - "8082:8082"
      # 将容器的 9099 端口映射到主机的 9099 端口
      - "9099:9099"
    restart: unless-stopped
```
启动后，你可以通过 `http://<主机IP>:8082` 访问UI，并将客户端代理设置为 `<主机IP>:9099`。

### 2.2. 二进制部署

适用于不使用 Docker 的环境。

1.  **编译**: 在项目根目录下运行 `go build -o liuproxy-gateway ./cmd/local`。
2.  **运行**: 将编译好的 `liuproxy-gateway` 文件和 `configs` 目录放在一起，然后执行 `./liuproxy-gateway --configdir=configs`。
```

