# Keykammer

For now, this is a basic client-server chat application using gRPC communication. The client sends a single "Hello from client!" message to the server, and the server responds with a success confirmation.

This is just a basic implementation to get started. Eventually, a user will be able to:

1. Allow the user to load any arbitrary file
2. A chat room named after the hash of this key file is created
3. Encryption is set up based on the key file
4. User can send encrypted messages in this chat room

Any user with the same file can connect to the room. There are no room lists or logs, only a person using that particular file can see if a room exists for it, and only a person with that particular file can decrypt the communications.

## Prerequisites

- Go 1.19 or later
- Protocol Buffers compiler (`protoc`)
- gRPC Go plugins

### Install dependencies

```bash
# Install protoc (varies by OS)
# On macOS:
brew install protobuf

# On Ubuntu/Debian:
sudo apt install protobuf-compiler

# Install Go plugins for protoc
go install google.golang.org/protobuf/cmd/protoc-gen-go@latest
go install google.golang.org/grpc/cmd/protoc-gen-go-grpc@latest
```

## Setup

1. Clone the repository:
```bash
git clone https://github.com/drewherron/keykammer.git
cd keykammer
```

2. Install dependencies:
```bash
go mod tidy
```

## Running

### Start the server
```bash
go run main.go -server
```

The server will listen on port 9999.

### Run the client
In a separate terminal:
```bash
go run main.go
```

The client will connect to the server, send a test message, and display the result.

## Expected Output

**Server terminal:**
```
Server listening on :9999
Received message: Hello from client!
```

**Client terminal:**
```
Message sent successfully: true
```

## Project Structure

```
keykammer/
├── proto/
│   ├── chat.proto          # Protocol Buffer definitions
│   ├── chat.pb.go          # Generated protobuf code
│   └── chat_grpc.pb.go     # Generated gRPC code
├── main.go                 # Server and client implementation
├── go.mod                  # Go module file
├── go.sum                  # Go module checksums
└── README.md              # This file
```
