#FROM golang:1.19 as build
#
#ENV GOPROXY=https://goproxy.cn \
#    GO111MODULE=on \
#    CGO_ENABLED=0 \
#    GOOS=linux \
#    GOARCH=amd64
#
#WORKDIR /build
#
#COPY go.mod .
#COPY go.sum .
#RUN go mod tidy
#
#COPY . .
#
#RUN go build -o ycnid .

#FROM scratch as deploy
#
#COPY --from=build /build/ycnid /
#
#CMD ["/ycnid"]
FROM busybox

COPY ./ycnid /

CMD ["/ycnid"]