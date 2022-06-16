FROM golang:alpine as builder

COPY . .

COPY docker-entrypoint.sh /run/

RUN apk add --no-cache tzdata && CGO_ENABLED=0 go build -o /run/app -ldflags="-w -s"

FROM alpine

ENV TZ=Asia/Shanghai

COPY --from=builder /run /run

WORKDIR /run

CMD ["/run/docker-entrypoint.sh"]