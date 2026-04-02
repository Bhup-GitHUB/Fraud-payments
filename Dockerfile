FROM golang:1.25 AS build

WORKDIR /app

COPY go.mod go.sum ./
RUN go mod download

COPY . .

ARG SERVICE

RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o /out/app ./cmd/${SERVICE}

FROM alpine:3.20

WORKDIR /app

COPY --from=build /out/app /app/app
COPY migrations /app/migrations

ENV MIGRATIONS_DIR=/app/migrations

CMD ["/app/app"]
