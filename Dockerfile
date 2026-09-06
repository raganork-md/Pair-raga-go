FROM golang:1.21-alpine

WORKDIR /app

# Go modules ഡൗൺലോഡ് ചെയ്യാൻ
COPY go.mod ./
RUN go mod download || true

COPY . .

# App ബിൽഡ് ചെയ്യൽ
RUN go build -o bot main.go

EXPOSE 8080

CMD ["./bot"]
