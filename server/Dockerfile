# 构建阶段固定跑在构建机自己的平台上，用 Go 交叉编译出目标平台的二进制（纯 Go，无 CGO），
# 打多架构镜像（linux/amd64 + linux/arm64）时不需要 QEMU 模拟，速度快很多。
FROM --platform=$BUILDPLATFORM golang:1.26-alpine AS builder

ARG GOPROXY=https://goproxy.cn,direct
ARG VERSION=dev
ARG TARGETOS
ARG TARGETARCH

ENV CGO_ENABLED=0 \
    GOPROXY=${GOPROXY}

WORKDIR /src

COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN mkdir -p /out/data && chown 65532:65532 /out/data && \
    GOOS=${TARGETOS:-linux} GOARCH=${TARGETARCH} go build -trimpath -ldflags="-s -w -X main.version=${VERSION}" -o /out/nuan-server ./cmd/server

# 运行镜像说明：
# 1. 代码 internal/chat/service.go 调用了 time.LoadLocation("Asia/Shanghai")，
#    gcr.io/distroless/static-debian12 已内置 Debian 的 tzdata（/usr/share/zoneinfo）
#    与 ca-certificates（访问上游 HTTPS 接口必需），无需额外拷贝时区和根证书。
# 2. :nonroot 变体默认以非特权用户 nonroot (UID/GID 65532) 运行。
# 3. distroless 镜像不含 shell 及 curl/wget，故不配置 HEALTHCHECK（请在编排层探测 /healthz）。
FROM gcr.io/distroless/static-debian12:nonroot

COPY --from=builder --chown=65532:65532 /out/data /data
COPY --from=builder /out/nuan-server /nuan-server

VOLUME /data
ENV NUAN_DATA_DIR=/data
EXPOSE 8080

USER nonroot:nonroot
ENTRYPOINT ["/nuan-server"]
