FROM node:18.20-alpine AS web-build
WORKDIR /src/web
COPY web/package*.json ./
RUN npm ci
COPY web/ ./
RUN npm run build

FROM alpine:3.22 AS crush-download
ARG TARGETARCH=amd64
ARG CRUSH_VERSION=0.91.0
ARG CRUSH_ARCHIVE_URL=
RUN apk add --no-cache ca-certificates \
    && case "${TARGETARCH}" in \
      amd64) archive_arch=x86_64; checksum=74afc41d03243894b5221f03b1bbc4032f1a219671ec9116148946dc2af4c708 ;; \
      arm64) archive_arch=arm64; checksum=bd9a88dba0c694bf63f679da3e7f0adef86125d35b466c8e30fadfe8bd9548f6 ;; \
      *) echo "unsupported Crush architecture: ${TARGETARCH}" >&2; exit 1 ;; \
    esac \
    && archive_url="${CRUSH_ARCHIVE_URL:-https://github.com/charmbracelet/crush/releases/download/v${CRUSH_VERSION}/crush_${CRUSH_VERSION}_Linux_${archive_arch}.tar.gz}" \
    && wget -q -O /tmp/crush.tar.gz "${archive_url}" \
    && echo "${checksum}  /tmp/crush.tar.gz" | sha256sum -c - \
    && tar -xzf /tmp/crush.tar.gz -C /usr/local/bin --strip-components=1 "crush_${CRUSH_VERSION}_Linux_${archive_arch}/crush" \
    && chmod 0755 /usr/local/bin/crush

FROM golang:1.25.13-alpine AS go-build
ARG GOPROXY=https://goproxy.cn,direct
ENV GOPROXY=${GOPROXY}
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY cmd/ cmd/
COPY internal/ internal/
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /velin ./cmd/velin

FROM alpine:3.22
RUN apk add --no-cache ca-certificates ffmpeg && addgroup -S velin && adduser -S -G velin velin
WORKDIR /app
COPY --from=go-build /velin /app/velin
COPY --from=web-build /src/web/dist /app/web/dist
COPY --from=crush-download /usr/local/bin/crush /usr/local/bin/crush
RUN mkdir /app/data && chown -R velin:velin /app
USER velin
ENV VELIN_ADDR=0.0.0.0:8377 VELIN_DATA_DIR=/app/data VELIN_WEB_DIST=/app/web/dist VELIN_HOST_PORT_ADDR=127.0.0.1
VOLUME ["/app/data"]
EXPOSE 8377
ENTRYPOINT ["/app/velin"]
