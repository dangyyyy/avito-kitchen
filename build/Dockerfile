FROM golang:1.25-alpine AS builder

WORKDIR /src

COPY . .

# `go mod download` is deliberately absent: it fetches every module in the graph,
# including the build-time dependencies of the oapi-codegen tool, which the
# services never import. `go build` fetches only what is actually imported, and
# the cache mounts keep repeat builds fast.
ARG SERVICE
RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=0 go build -trimpath -o /out/app ./cmd/${SERVICE}

FROM alpine:3.21

RUN adduser -D -u 10001 app
USER app

COPY --from=builder /out/app /usr/local/bin/app

ENTRYPOINT ["/usr/local/bin/app"]
