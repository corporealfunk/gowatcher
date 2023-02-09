FROM golang:1.19 AS builder
WORKDIR /go/src/github.com/corporealfunk/gowatcher/
COPY . .
RUN go get
RUN CGO_ENABLED=0 go build -a -installsuffix cgo -o gowatcher .

FROM jrottenberg/ffmpeg
RUN mkdir -p /media/watch
ENV BASE_DIR=/media/watch
WORKDIR /root/
COPY --from=builder /go/src/github.com/corporealfunk/gowatcher/gowatcher ./
ENTRYPOINT ["./gowatcher"]
