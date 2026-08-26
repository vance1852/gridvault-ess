FROM golang:1.22.5-bookworm AS builder
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/gridvault ./cmd/server

FROM gcr.io/distroless/static-debian12:nonroot
WORKDIR /app
COPY --from=builder /out/gridvault /app/gridvault
ENV GRIDVAULT_ADDR=:8080 GRIDVAULT_DB_PATH=/tmp/gridvault.db
EXPOSE 8080
ENTRYPOINT ["/app/gridvault"]
