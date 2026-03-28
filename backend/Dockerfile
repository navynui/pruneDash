# Use the official Golang image
FROM golang:1.24-alpine

# Set the working directory
WORKDIR /app

# Install build essentials
RUN apk add --no-cache gcc musl-dev util-linux bash

# Copy the go mod and sum files
COPY go.mod go.sum ./

# Download dependencies
RUN go mod download

# Copy the source code
COPY . .

# Build the application
RUN go build -o main .

# Expose the port
EXPOSE 3333

# Run the application
CMD ["./main"]
