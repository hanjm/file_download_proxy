FROM golang:1.8.4-alpine3.6
RUN apk -U add lsof git aria2
WORKDIR /go
ENV GOPATH=/go
RUN go get -v github.com/mxue12138/file_download_proxy/...
WORKDIR /go/src/github.com/mxue12138/file_download_proxy
RUN go build -o fdp
EXPOSE 8080
CMD ["./fdp","-limit","100"]
