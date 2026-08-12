FROM node:18.20-alpine AS web-build
WORKDIR /src/web
COPY web/package*.json ./
RUN npm ci
COPY web/ ./
RUN npm run build

FROM golang:1.24-alpine AS go-build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY cmd/ cmd/
COPY internal/ internal/
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /velin ./cmd/velin

FROM alpine:3.22
RUN apk add --no-cache ca-certificates tzdata && addgroup -S velin && adduser -S -G velin velin
WORKDIR /app
COPY --from=go-build /velin /app/velin
COPY --from=web-build /src/web/dist /app/web/dist
RUN mkdir /app/data && chown -R velin:velin /app
USER velin
ENV VELIN_ADDR=:8080 VELIN_DATA_DIR=/app/data VELIN_WEB_DIST=/app/web/dist
VOLUME ["/app/data"]
EXPOSE 8080
ENTRYPOINT ["/app/velin"]
