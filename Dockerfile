FROM golang:1.25-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -o /crowdin-stats ./cmd/crowdin-stats

FROM alpine:3.20
RUN apk add --no-cache ca-certificates && \
    addgroup -S -g 10001 crowdin-stats && \
    adduser -S -D -H -u 10001 -G crowdin-stats crowdin-stats && \
    mkdir -p /data && chown crowdin-stats:crowdin-stats /data
WORKDIR /
COPY --from=build /crowdin-stats /crowdin-stats
COPY static /static
USER crowdin-stats:crowdin-stats
ENTRYPOINT ["/crowdin-stats"]
