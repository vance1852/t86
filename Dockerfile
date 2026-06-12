FROM golang:1.23 AS build

WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -o /out/app .

FROM debian:bookworm-slim
WORKDIR /app
COPY --from=build /out/app ./app
EXPOSE 7653
ENTRYPOINT ["./app"]
