FROM golang:1.27 AS build

WORKDIR /app

COPY go.* ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 go build -o /bin/mourncdn .
RUN mkdir -p /runtime-data/assets /runtime-data/tmp

FROM gcr.io/distroless/static-debian13:nonroot

COPY --from=build --chown=nonroot:nonroot /runtime-data/ /app/data
WORKDIR /app

COPY --from=build /bin/mourncdn /app/mourncdn
COPY config.yml /app/config.yml

EXPOSE 9743

ENTRYPOINT ["/app/mourncdn"]
