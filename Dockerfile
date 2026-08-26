FROM golang:1.24.6-bookworm AS builder

WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN GOTOOLCHAIN=local CGO_ENABLED=0 go test ./... -run '^$' -count=1 \
    && GOTOOLCHAIN=local CGO_ENABLED=0 go build -trimpath -ldflags='-s -w' -o /out/cotton-evidence-ledger ./cmd/server \
    && mkdir -p /tmp/data

FROM gcr.io/distroless/base-debian12:nonroot
WORKDIR /app
COPY --from=builder /out/cotton-evidence-ledger /app/cotton-evidence-ledger
COPY --from=builder --chown=nonroot:nonroot /tmp/data /data
ENV COTTON_ADDR=:8080
ENV COTTON_DATABASE=/data/cotton-evidence.db
EXPOSE 8080
HEALTHCHECK --interval=5s --timeout=3s --start-period=5s --retries=10 CMD ["/app/cotton-evidence-ledger", "healthcheck"]
USER nonroot:nonroot
ENTRYPOINT ["/app/cotton-evidence-ledger"]
