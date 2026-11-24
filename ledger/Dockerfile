# syntax=docker/dockerfile:1
# Контекст сборки - корень репозитория (так же, как у gateway).
# Сборка из корня: docker build -f ledger/Dockerfile -t cashapp-ledger .

FROM golang:1.26-alpine AS builder
WORKDIR /src
COPY ledger/go.mod ledger/go.sum ./ledger/
RUN cd ledger && go mod download
COPY ledger/ ./ledger/
WORKDIR /src/ledger
RUN CGO_ENABLED=0 go build -trimpath -ldflags "-s -w" -o /out/ledger ./cmd/ledger

FROM alpine:latest AS runner
RUN addgroup -S app && adduser -S -G app app
WORKDIR /app
COPY --from=builder /out/ledger /app/ledger
COPY --from=builder /src/ledger/budgets.json /app/budgets.json
USER app
ENTRYPOINT ["/app/ledger"]
CMD ["-seed", "budgets.json"]
