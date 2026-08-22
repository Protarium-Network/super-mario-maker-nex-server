# syntax=docker/dockerfile:1

FROM golang:1.25-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
COPY nex-protocols-common-go-patch/go.mod nex-protocols-common-go-patch/go.sum ./nex-protocols-common-go-patch/
RUN go mod download
COPY . .
ARG BUILD_STRING=development
RUN CGO_ENABLED=0 go build -trimpath -ldflags "-X main.serverBuildString=${BUILD_STRING}" -o /out/super-mario-maker .

FROM alpine:3.22
RUN addgroup -S server && adduser -S -G server server
WORKDIR /app
COPY --from=build /out/super-mario-maker /app/server
USER server
ENTRYPOINT ["/app/server"]
