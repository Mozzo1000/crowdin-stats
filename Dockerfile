FROM golang:1.25-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -o /crowdin-stats ./cmd/crowdin-stats

FROM alpine:3.20
WORKDIR /
COPY --from=build /crowdin-stats /crowdin-stats
COPY static /static
ENTRYPOINT ["/crowdin-stats"]
