FROM golang:alpine as builder

WORKDIR $GOPATH/src/controller/
COPY go.mod go.sum main.go ./
RUN go build -o /go/bin/controller ./main.go

FROM scratch

WORKDIR /go/bin/
COPY --from=alpine /tmp /tmp
COPY --from=builder /go/bin/controller ./controller
COPY k8s-resources/tester.yml ./
EXPOSE 8080

ENTRYPOINT ["./controller"]
