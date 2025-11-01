FROM golang:1.25.3 AS builder
WORKDIR /app
COPY ./go.mod .
COPY ./go.sum .
ENV GOPROXY=https://goproxy.cn,direct
RUN go mod tidy
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o sre-agent 
FROM alpine:latest
# 从构建阶段(builder)复制编译好的二进制文件到当前镜像
COPY --from=builder /app/sre-agent /sre-agent

# 设置容器的入口点。当容器启动时，它会执行/sre-agent这个程序。
ENTRYPOINT ["/sre-agent"]