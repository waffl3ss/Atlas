FROM golang:1.24-alpine AS builder

ARG VERSION=dev
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -ldflags "-s -w -X main.Version=${VERSION}" -o /atlas ./cmd/atlas/

FROM alpine:3.21

RUN apk add --no-cache ca-certificates tzdata su-exec

RUN adduser -D -h /home/atlas atlas
WORKDIR /app

COPY --from=builder /atlas /app/atlas
COPY --chown=atlas:atlas templates/ /app/templates/
COPY --chown=atlas:atlas static/ /app/static/
COPY entrypoint.sh /app/entrypoint.sh
RUN chmod +x /app/entrypoint.sh

EXPOSE 8443

ENTRYPOINT ["/app/entrypoint.sh"]
