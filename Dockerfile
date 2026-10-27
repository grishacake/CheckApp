FROM golang:1.26-alpine AS build
WORKDIR /src
COPY go.mod go.sum* ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -mod=readonly -o /server ./cmd/server

FROM golang:1.26-alpine AS goose-build
RUN CGO_ENABLED=0 go install \
    -tags "no_clickhouse no_libsql no_mssql no_mysql no_sqlite3 no_vertica no_ydb" \
    github.com/pressly/goose/v3/cmd/goose@v3.29.0

FROM alpine:3.23 AS runtime
RUN apk add --no-cache ca-certificates

FROM runtime AS migrations
COPY --from=goose-build /go/bin/goose /goose
COPY migrations /migrations
ENTRYPOINT ["/goose"]

FROM runtime AS backend
COPY --from=build /server /server
EXPOSE 8080
ENTRYPOINT ["/server"]
