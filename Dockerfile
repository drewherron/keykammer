# syntax=docker/dockerfile:1

ARG VERSION=dev
ARG BUILD_TIME=unknown
ARG GIT_COMMIT=unknown

FROM golang:1.22-alpine AS build
WORKDIR /src

COPY go.mod go.sum ./
RUN go mod download

COPY . .

ARG VERSION
ARG BUILD_TIME
ARG GIT_COMMIT
RUN CGO_ENABLED=0 go build \
    -trimpath \
    -buildvcs=false \
    -ldflags "-s -w \
        -X 'main.Version=${VERSION}' \
        -X 'main.BuildTime=${BUILD_TIME}' \
        -X 'main.GitCommit=${GIT_COMMIT}'" \
    -o /out/keykammer .

FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /out/keykammer /usr/local/bin/keykammer
EXPOSE 53952
ENTRYPOINT ["/usr/local/bin/keykammer"]
