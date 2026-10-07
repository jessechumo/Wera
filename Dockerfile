# Wera container image per PLAN.md section 13: multi-stage build producing
# a static binary on distroless. Secrets (CORAL_API_KEY) come from the
# environment at runtime; profile/ is mounted read-only, never baked in.
FROM golang:1.25-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/wera ./cmd/wera

FROM gcr.io/distroless/static:nonroot
WORKDIR /app
COPY --from=build /out/wera /app/wera
COPY --from=build /src/config /app/config
ENV HTTP_ADDR=:8080
USER nonroot
EXPOSE 8080
ENTRYPOINT ["/app/wera"]
CMD ["serve"]
