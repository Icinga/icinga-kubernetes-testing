FROM golang:alpine as builder

WORKDIR $GOPATH/src/tester/
COPY go.mod go.sum main.go ./
RUN go build -o /go/bin/tester ./main.go

FROM ubuntu:latest

WORKDIR /go/bin/
COPY --from=alpine /tmp /tmp
COPY --from=builder /go/bin/tester ./tester

ENTRYPOINT ["./tester"]
