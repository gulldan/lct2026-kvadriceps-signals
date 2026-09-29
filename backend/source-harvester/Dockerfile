# Контекст сборки — только эта папка: сервис выносится в отдельный репозиторий как есть.
FROM golang:1.27.1-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN --mount=type=cache,id=source-harvester-go-mod,target=/go/pkg/mod go mod download
COPY cmd ./cmd
COPY internal ./internal
RUN --mount=type=cache,id=source-harvester-go-mod,target=/go/pkg/mod --mount=type=cache,id=source-harvester-go-build,target=/root/.cache/go-build \
    CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/app ./cmd/source-harvester

FROM gcr.io/distroless/static-debian13:nonroot
WORKDIR /
COPY --from=build /out/app /app
USER nonroot:nonroot
ENTRYPOINT ["/app"]
