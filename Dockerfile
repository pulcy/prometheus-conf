FROM alpine:3.3

ADD ./prometheus-conf /app/

# Configure volumns
VOLUME ["/data/config"]

# Export ports
EXPOSE 80

# Start the config builder
ENTRYPOINT ["/app/prometheus-conf"]
