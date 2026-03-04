FROM golang:alpine AS go-build
WORKDIR /stream-sniff
ENV GOPROXY=direct
ENV GOSUMDB=off
COPY . /stream-sniff
RUN apk add --no-cache git
RUN go build

FROM alpine:3.22
RUN apk add --no-cache ca-certificates
COPY --from=go-build /stream-sniff/stream-sniff /stream-sniff/stream-sniff
COPY --from=go-build /stream-sniff/.env.production /stream-sniff/.env.production

ENV APP_ENV=production

WORKDIR /stream-sniff
ENTRYPOINT ["/stream-sniff/stream-sniff"]
