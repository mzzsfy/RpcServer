FROM golang:alpine as builder

ENV GO111MODULE=auto GOFLAGS='-mod=mod' TZ=Asia/Shanghai GOPATH='/gopath'

COPY . .

COPY docker-entrypoint.sh /run/

RUN apk add --no-cache git && CGO_ENABLED=0 go build -o /run/app -ldflags="-w -s"

FROM alpine

RUN apk add --no-cache tzdata

ENV TZ=Asia/Shanghai

COPY --from=builder /run /run

WORKDIR /run

CMD ["/run/docker-entrypoint.sh"]