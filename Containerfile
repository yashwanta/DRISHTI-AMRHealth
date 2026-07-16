FROM docker.io/library/node:24-alpine AS frontend
WORKDIR /src/frontend
COPY frontend/package*.json ./
RUN npm ci
COPY frontend ./
RUN npm run build

FROM docker.io/library/golang:1.25-alpine AS backend
WORKDIR /src/backend
COPY backend/go.mod backend/go.sum ./
RUN go mod download
COPY backend ./
RUN go build -o /out/drishti-amr-health .

FROM docker.io/library/alpine:3.21
RUN apk add --no-cache openssh-client
LABEL org.opencontainers.image.title="DRISHTI - AMR Health"
LABEL org.opencontainers.image.description="Local Go and React AMR health dashboard with RDS proxy"
WORKDIR /app
COPY --from=backend /out/drishti-amr-health /app/drishti-amr-health
COPY --from=frontend /src/frontend/dist /app/frontend/dist
COPY data/config/api-connections.example.json /app/data/config/api-connections.example.json
ENV PORT=8090
ENV DRISHTI_STATIC_DIR=/app/frontend/dist
ENV DRISHTI_API_CONFIG=/app/data/config/api-connections.json
VOLUME ["/app/data"]
EXPOSE 8090
CMD ["/app/drishti-amr-health"]