FROM golang:1.23-bookworm AS builder
WORKDIR /app
COPY go.mod .
COPY main.go .
RUN go build -ldflags="-s -w" -o /vm-app .

FROM debian:bookworm-slim
RUN apt-get update && apt-get install -y --no-install-recommends ca-certificates && rm -rf /var/lib/apt/lists/*
COPY --from=builder /vm-app /usr/local/bin/vm-app
ENTRYPOINT ["vm-app"]
