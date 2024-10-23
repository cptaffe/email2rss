FROM busybox:1.35

# Create a non-root user to own the files and run our server
RUN adduser -D static
USER static
WORKDIR /home/static

RUN echo 'healthy' >/home/static/healthz

# Run BusyBox httpd
CMD ["busybox", "httpd", "-f", "-v", "-p", "8080"]
