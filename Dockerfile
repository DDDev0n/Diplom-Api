FROM golang:1.23-alpine AS build

WORKDIR /src
RUN apk add --no-cache ca-certificates

COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -o /out/api ./cmd/api
RUN CGO_ENABLED=0 GOOS=linux go build -o /out/worker ./cmd/worker

FROM alpine:3.21 AS api
WORKDIR /app
RUN apk add --no-cache ca-certificates
COPY --from=build /out/api /app/api
EXPOSE 8000
CMD ["/app/api"]

FROM alpine:3.21 AS worker
WORKDIR /app
RUN apk add --no-cache ca-certificates
COPY --from=build /out/worker /app/worker
CMD ["/app/worker"]
