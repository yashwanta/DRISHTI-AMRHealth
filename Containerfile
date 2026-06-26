FROM docker.io/library/nginx:1.27-alpine

LABEL org.opencontainers.image.title="DRISHTI - AMR Health"
LABEL org.opencontainers.image.description="Local AMR health, discovery, log investigation, and Wi-Fi heat map dashboard"

COPY index.html /usr/share/nginx/html/index.html
COPY styles.css /usr/share/nginx/html/styles.css
COPY app.js /usr/share/nginx/html/app.js
COPY assets /usr/share/nginx/html/assets

EXPOSE 80
