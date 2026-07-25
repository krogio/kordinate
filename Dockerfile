# Multi-stage production build for kordinate.
#
# kore is a private module: build injects a GitHub token via a BuildKit secret
# so `go mod download` can fetch github.com/krogio/kore at the pinned version.
# The dev `replace … => /kore` overlay lives only in the container go.work
# (Dockerfile.dev), never in go.mod — dropped defensively here regardless.
FROM golang:1.26-alpine AS build
RUN apk add --no-cache git
ENV GOPRIVATE=github.com/krogio/*
WORKDIR /src

COPY go.mod go.sum ./
RUN --mount=type=secret,id=ghtoken \
    sed -i '/^replace github.com\/krogio\/kore/d' go.mod && \
    git config --global url."https://x-access-token:$(cat /run/secrets/ghtoken)@github.com/".insteadOf "https://github.com/" && \
    go mod download && \
    git config --global --unset-all url."https://x-access-token:$(cat /run/secrets/ghtoken)@github.com/".insteadOf

COPY . .
RUN sed -i '/^replace github.com\/krogio\/kore/d' go.mod && \
    CGO_ENABLED=0 go build -o /out/kordinate ./cmd/kordinate

FROM alpine:3.20
RUN apk add --no-cache ca-certificates tzdata && adduser -D -u 20000 app
USER app
COPY --from=build /out/kordinate /usr/local/bin/kordinate
EXPOSE 8080
ENTRYPOINT ["kordinate"]
