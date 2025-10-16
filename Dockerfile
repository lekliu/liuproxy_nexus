#  FILE: Dockerfile

# --- STAGE 1: Build Stage ---
FROM golang:1.24-alpine AS builder

WORKDIR /app

# 复制模块文件并下载依赖
COPY go.mod go.sum ./

RUN export GOPROXY=https://goproxy.cn,direct && go mod download

# 复制所有源代码
COPY . .

# 只编译 local 可执行文件
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /app/bin/liuproxy-nexus ./cmd/local


# --- STAGE 2: Final Image ---
FROM alpine:latest

WORKDIR /app

# 【新增】更新包列表并安装 curl 和 iptables，--no-cache 选项可以在同一层中完成更新和安装，并清除缓存，保持镜像苗条
RUN apk update && apk add --no-cache curl iptables

# 复制编译好的二进制文件和入口脚本
COPY --from=builder /app/bin/liuproxy-nexus .
COPY scripts/entrypoint.sh .
RUN chmod +x ./entrypoint.sh

# 只复制默认配置文件
COPY configs/ ./configs/

# 暴露服务端口
EXPOSE 8082 9099

# 将容器的入口点设置为我们的脚本
ENTRYPOINT ["./entrypoint.sh"]

# CMD 现在作为 ENTRYPOINT 的默认参数，如果 ENTRYPOINT 最后没有 exec，它会被执行
# 但因为我们用了 exec，这里的内容实际上不会被使用，但保留是好的实践
CMD ["./liuproxy-nexus", "--configdir", "configs"]