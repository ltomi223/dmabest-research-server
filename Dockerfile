FROM golang:1.23-alpine AS build
WORKDIR /app
COPY main.go .
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o /out/dmabest-research-server main.go

FROM alpine:3.20
RUN adduser -D -u 10001 appuser
WORKDIR /app
COPY --from=build /out/dmabest-research-server /app/dmabest-research-server
RUN mkdir -p /app/data/cache && chown -R appuser:appuser /app
USER appuser
ENV PORT=8787
ENV DMABEST_CACHE_DIR=/app/data/cache
EXPOSE 8787
CMD ["/app/dmabest-research-server"]
