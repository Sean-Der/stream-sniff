FROM golang:alpine AS go-build
WORKDIR /streamsniff
ENV GOPROXY=direct
ENV GOSUMDB=off
COPY . /streamsniff
RUN apk add --no-cache git
RUN go build

FROM alpine:3.22
RUN apk add --no-cache ca-certificates
COPY --from=go-build /streamsniff/streamsniff /streamsniff/streamsniff
COPY --from=go-build /streamsniff/.env.production /streamsniff/.env.production

ENV APP_ENV=production

WORKDIR /streamsniff
ENTRYPOINT ["/streamsniff/streamsniff"]
