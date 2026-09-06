FROM golang:1.27 AS build

WORKDIR /app

COPY go.* ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 go build -o /bin/mourncdn .

FROM gcr.io/distroless/static-debian13:nonroot

WORKDIR /app

COPY --from=build /bin/mourncdn /app/mourncdn
COPY config.yml /app/config.yml

EXPOSE 9743

ENTRYPOINT ["/app/mourncdn"]
