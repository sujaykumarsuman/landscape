# landscape — read-only GitOps/cluster console. Static Go binary + embedded UI.
FROM golang:1.26-alpine AS build
ARG VERSION=dev
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath \
      -ldflags="-s -w -X main.Version=${VERSION}" \
      -o /out/landscape ./cmd/landscape

FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /out/landscape /usr/local/bin/landscape
USER nonroot:nonroot
EXPOSE 8080
ENTRYPOINT ["/usr/local/bin/landscape"]
