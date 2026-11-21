# ---- 构建阶段：只负责编译出二进制 ----
FROM golang:1.21-alpine AS builder

WORKDIR /src

# 先拷依赖清单，利用 Docker 层缓存：源码变了也不用重新下依赖
COPY go.mod go.sum ./
RUN go mod download

COPY . .

# CGO_ENABLED=0 编出静态二进制，才能直接跑在精简镜像里
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/node .

# ---- 运行阶段：只放二进制，不带源码和编译器 ----
FROM alpine:3.19

RUN apk add --no-cache ca-certificates tzdata

WORKDIR /app
COPY --from=builder /out/node /app/node

# 链数据落在数据卷里，容器重建不丢链
VOLUME ["/data"]

EXPOSE 8001

# 默认跑单节点模式；想跑演示可覆盖：docker run ... -demo=true
ENTRYPOINT ["/app/node"]
CMD ["-demo=false", "-port=:8001", "-data=/data/chain.db"]
