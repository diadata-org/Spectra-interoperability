# hyperlane-validator/Dockerfile
FROM gcr.io/abacus-labs-dev/hyperlane-agent:agents-v1.0.0

# Set the user to root
USER root

RUN mkdir -p /app/config/
# Copy default configuration files to a different directory inside the container
COPY ./hyperlane/agent-config.docker.json /app/config/agent-config.json

# Create the necessary directory for the database
RUN mkdir -p /etc/data/db

ENTRYPOINT ["./relayer"]
