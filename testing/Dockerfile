FROM golang:1.23-alpine AS builder

WORKDIR /app
COPY go.mod ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 go build -o /harness ./cmd/harness

FROM alpine:3.20
RUN apk add --no-cache ca-certificates bash curl
COPY --from=builder /harness /usr/local/bin/harness
COPY scripts/ /scripts/
COPY scenarios/ /scenarios/
RUN chmod +x /scripts/*.sh

ENV HARNESS_BIN=/usr/local/bin/harness
ENV ARTERIA_MLLP_HOST=ingestion
ENV ARTERIA_API_URL=http://api:8080

ENTRYPOINT ["/scripts/run-tests.sh"]
CMD ["all"]
