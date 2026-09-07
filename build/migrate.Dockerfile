FROM golang:1.25-alpine AS builder

RUN go install github.com/pressly/goose/v3/cmd/goose@v3.27.3

FROM alpine:3.21

COPY --from=builder /go/bin/goose /usr/local/bin/goose
COPY migrations /migrations

ENTRYPOINT ["goose"]
CMD ["up"]
