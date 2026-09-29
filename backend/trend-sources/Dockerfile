FROM golang:1.27.1-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN mkdir /data
RUN CGO_ENABLED=0 go build -trimpath -ldflags='-s -w' -o /app .
FROM gcr.io/distroless/static-debian13:nonroot
COPY --from=build /app /app
COPY --from=build --chown=65532:65532 /data /data
ENTRYPOINT ["/app"]
