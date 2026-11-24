# syntax=docker/dockerfile:1
# Контекст сборки - КОРЕНЬ репозитория: gateway зависит от соседнего модуля ledger через replace => ../ledger.
# Сборка из корня: docker build -f gateway/Dockerfile -t cashapp-gateway .

FROM golang:1.26-alpine AS builder
WORKDIR /src
# сначала только манифесты модулей: слой с зависимостями кешируется, пока go.mod/go.sum не менялись
COPY ledger/go.mod ledger/go.sum ./ledger/
COPY gateway/go.mod gateway/go.sum ./gateway/
RUN cd gateway && go mod download
COPY ledger/ ./ledger/
COPY gateway/ ./gateway/
WORKDIR /src/gateway
RUN CGO_ENABLED=0 go build -trimpath -ldflags "-s -w" -o /out/gateway .

FROM alpine:latest AS runner
RUN addgroup -S app && adduser -S -G app app
WORKDIR /app
COPY --from=builder /out/gateway /app/gateway
USER app
EXPOSE 8080
# exec-форма: процесс получает SIGTERM от docker stop и завершается через graceful shutdown
ENTRYPOINT ["/app/gateway"]
