FROM golang:1.21-alpine

WORKDIR /app

# All files copy ചെയ്യുക
COPY . .

# Dependencies auto-download ചെയ്ത് ബിൽഡ് ചെയ്യുക
RUN go mod tidy
RUN CGO_ENABLED=0 GOOS=linux go build -o bot main.go

EXPOSE 8080

CMD ["./bot"]
