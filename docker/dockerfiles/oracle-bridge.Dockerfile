FROM golang:1.22 as build


WORKDIR $GOPATH/src/oracleservice
COPY . .



WORKDIR /go/src/oracleservice/cmd/updater

RUN go mod tidy

RUN GOOS=linux GOARCH=amd64 go build -o /go/bin/updater

FROM gcr.io/distroless/base

COPY --from=build /go/bin/updater /bin/updater
 
CMD ["/bin/updater"]
