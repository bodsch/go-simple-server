# syntax=docker/dockerfile:1

FROM gcr.io/golang:1.22-alpine AS build
WORKDIR /src
COPY go.mod ./
RUN go mod download
COPY cmd/ ./cmd/
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath -ldflags="-s -w" -o /out/simple-api ./cmd/server

FROM gcr.io/distroless/static:nonroot

ENV PORT=8080
EXPOSE 8080
COPY --from=build /out/simple-api /simple-api
USER nonroot:nonroot
ENTRYPOINT ["/simple-api"]
