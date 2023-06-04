FROM golang:1.17-alpine

WORKDIR /app

COPY main.go .

COPY go.mod .

COPY go.sum .

RUN go build -o main .

CMD ["./main"]