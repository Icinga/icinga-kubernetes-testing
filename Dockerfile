FROM golang:alpine as builder

RUN apk update && apk add --no-cache git
WORKDIR $GOPATH/src/icinga-kubernetes-testing/
COPY . .
RUN go build -o /go/bin/icinga-kubernetes-testing ./cmd/icinga-kubernetes-testing/main.go

FROM scratch

WORKDIR /go/bin/
COPY --from=alpine /tmp /tmp
COPY --from=builder /go/bin/icinga-kubernetes-testing ./icinga-kubernetes-testing
EXPOSE 8080

ENTRYPOINT ["./icinga-kubernetes-testing"]
