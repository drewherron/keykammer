# Keykammer

**File-Based P2P Encrypted Chatrooms**

Keykammer is a peer-to-peer encrypted chat application where any arbitrary file serves as both the room identifier and encryption key. Users with the same file can join the same secure chatroom from anywhere on the internet. Without the file, there is no way to connect to the room, decrypt the messages, or even know that the room exists.

The original idea was for any two users to run a program using an arbitrary file, and connect to a private chatroom based on that file. Of course, that alone would not provide any required addresses to complete the connection, so an intermediate "discovery server" was added to route users to the correct IP address. The discovery server can be bypassed by providing the other user's IP address directly.

### How It Works

This might best be explained by describing an instance of actual use.

Alice runs the program using a picture of her dog as the keyfile: `./keykammer -keyfile fluffy.jpg`. A SHA-256 hash is created from the file, to be used as both the room ID and the key to encrypt any messages. On a remote discovery server, no existing room is found with that room ID, so the room becomes `LISTED` (i.e., Alice's IP address is associated with that room ID).

Bob runs the program with the same file. Hashing the file provides him with the room ID. The remote discovery server looks up Alice's IP address by this room ID, and connects Bob *directly* to Alice's room. The two users now have a private chatroom, directly encrypted using a key based on their arbitrary keyfile.

By default, rooms have a capacity of only two users (this can be changed by command line argument). After Bob joins the room, the listing is removed from the discovery server. There is now no evidence on the discovery server that the room ever existed. When some third user runs the program using the same keyfile, the process starts over and a new room is created.

Alice could have entirely avoided listing the room on the discovery server by using `-discovery-server none` (or any invalid address). This starts the chat server locally, without using any discovery server. In this case, Bob would need Alice's IP address, and would run the program using `-connect IP:PORT`.

If you want to use a discovery server but you don't trust my central discovery server, you can run your own using `-discovery-server-mode`. Then supply the address to that server when running a client with `-discovery-server ADDRESS`.

### Features

- **File-based room discovery** - Any file creates a unique chatroom ID
- **Internet-wide P2P chat** - Users anywhere can connect using discovery server
- **Real-time bidirectional messaging** - Send and receive messages instantly
- **End-to-end encryption** - AES-256-GCM encryption of all chat messages
- **Terminal User Interface (TUI)** - Full-featured chat interface with user list and status
- **Username management** - Unique usernames per room with conflict resolution
- **Automatic server/client detection** - First user becomes server, others join as clients
- **UPnP port forwarding** - Automatic NAT traversal for home networks
- **Room capacity management** - Configurable max users with auto-delete when full
- **Room status system** - LISTED/OPEN/CLOSED status with real-time updates
- **Privacy-focused discovery** - Zero-logging discovery server for maximum anonymity
- **Connection retry logic** - Handles network issues with exponential backoff
- **Discovery server** - HTTP REST API for room registration and lookup
- **Direct connections** - Bypass discovery server for local/known IP connections

### Quick Start

**1. Run a discovery server (on a VM or public server):**

*Linux/macOS:*
```bash
./keykammer -discovery-server-mode -port 53952
```

*Windows:*
```cmd
keykammer.exe -discovery-server-mode -port 53952
```

**2. Start first chat instance:**

*Linux/macOS:*
```bash
./keykammer -keyfile myfile.txt -discovery-server http://your-server:53952
```

*Windows:*
```cmd
keykammer.exe -keyfile myfile.txt -discovery-server http://your-server:53952
```

**3. Join from another computer:**

*Linux/macOS:*
```bash
./keykammer -keyfile myfile.txt -discovery-server http://your-server:53952
```

*Windows:*
```cmd
keykammer.exe -keyfile myfile.txt -discovery-server http://your-server:53952
```

Both users can now chat in real-time. The first user automatically becomes the server, the second connects as a client. UPnP will attempt automatic port forwarding. When the room reaches capacity, all evidence of it is deleted from the discovery server.

### Usage Examples

**Basic chat (2 users max by default):**

*Linux/macOS:*
```bash
./keykammer -keyfile photo.jpg
```

*Windows:*
```cmd
keykammer.exe -keyfile photo.jpg
```

**Larger group chat (up to 5 users):**

*Linux/macOS:*
```bash
./keykammer -keyfile document.pdf -size 5
```

*Windows:*
```cmd
keykammer.exe -keyfile document.pdf -size 5
```

**Direct connection (bypass discovery):**

*Linux/macOS:*
```bash
./keykammer -keyfile myfile.txt -connect 192.168.1.100:53952
```

*Windows:*
```cmd
keykammer.exe -keyfile myfile.txt -connect 192.168.1.100:53952
```

**With password for additional security:**

*Linux/macOS:*
```bash
./keykammer -keyfile data.bin -password "additional secret"
```

*Windows:*
```cmd
keykammer.exe -keyfile data.bin -password "additional secret"
```

## Cryptographic Security

Keykammer implements a cryptographic system that provides both message confidentiality and authenticity. The security is entirely based on possession of the keyfile - without the exact file, neither room discovery nor message decryption is possible.

### Key Derivation Architecture

**1. Master Key Material**
- Any file of any size serves as the base key material
- Optional password can be provided for additional entropy
- Combined input: `file_content + password` (concatenated)

**2. Room ID Derivation**
```
room_id = SHA-256(salt + file_content + password)
salt = "keykammer-v1-salt" (constant across all clients)
```

**3. Encryption Key Derivation**
```
encryption_key = HKDF-SHA256(
    ikm: file_content + password,
    salt: "keykammer-v1-salt", 
    info: "keykammer-encryption-key",
    length: 32 bytes
)
```

The use of HKDF (HMAC-based Key Derivation Function) ensures that even if the same file is used multiple times, the derived keys have proper cryptographic properties and are uniformly distributed.

### Message Encryption

**Algorithm**: AES-256-GCM (Galois/Counter Mode)

- **Key size**: 256 bits (32 bytes) 
- **Authentication**: Built-in AEAD (Authenticated Encryption with Associated Data)
- **Nonce**: 96-bit random nonce generated per message
- **Tag**: 128-bit authentication tag automatically appended

**Encryption Process**:

1. Generate random 96-bit nonce using `crypto/rand`
2. Encrypt plaintext using AES-256-GCM with derived key and nonce
3. Authentication tag is automatically computed over ciphertext
4. Final message: `nonce || ciphertext || tag` (concatenated)

**Decryption Process**:

1. Extract nonce from first 12 bytes of message
2. Extract ciphertext+tag from remaining bytes  
3. Decrypt and verify authenticity in single GCM operation
4. Return plaintext only if authentication succeeds

### Security Properties

**Confidentiality**: Messages are encrypted with AES-256, providing security against all known classical attacks. Only holders of the exact keyfile (and password, if used) can decrypt messages.

**Authenticity**: GCM mode provides authenticated encryption - any tampering with ciphertext will cause decryption to fail. Recipients can be certain messages came from someone with the keyfile (and password, if used).

**Key Security**: The 256-bit encryption keys are derived using HKDF-SHA256, ensuring proper key distribution even from low-entropy input files. Keys never appear in logs or debugging output.

**Nonce Safety**: Each message uses a fresh random nonce, preventing nonce reuse attacks. The 96-bit nonce space provides adequate collision resistance for realistic usage.

### Threat Model

**What Keykammer protects against**:
- Network eavesdropping (passive surveillance)
- Message tampering (active attacks) 
- Discovery server compromise (messages remain encrypted)
- Room enumeration without keyfile (room IDs are unpredictable)

**What Keykammer does NOT protect against**:
- Keyfile/password compromise (anyone with both can decrypt all messages)
- Endpoint compromise (malware on user's computer)
- Traffic analysis (metadata like message timing/size is visible)
- Forward secrecy (past messages remain decryptable if key is compromised)

### Implementation Notes

The cryptographic implementation uses Go's standard `crypto/aes` and `crypto/cipher` packages, which provide constant-time implementations resistant to timing attacks. All encryption keys are derived using the standardized HKDF construction from RFC 5869.

Room IDs are computed using SHA-256 to ensure they are unpredictable without the keyfile, preventing room enumeration attacks. The consistent salt ensures that the same keyfile always produces the same room ID and encryption key, enabling reliable room discovery.

### Configuration

Keykammer supports YAML configuration files for setting default values, eliminating the need to specify command line arguments repeatedly.

**Automatic Configuration Loading:**
- Keykammer automatically looks for `keykammer.yaml` in the current directory
- If found, values from the config file become the new defaults
- Command line flags always override config file values
- If no config file exists, built-in defaults are used

**Configuration File Format:**
```yaml
# Connection Settings
port: 53952
discovery_server: "https://discovery.keykammer.com"
connect_direct: ""

# File and Security Settings  
keyfile: ""
password: ""

# Room Settings
max_users: 2

# Server Mode Settings
discovery_server_mode: false
```

**Usage Examples:**
```bash
# With config file - uses values from keykammer.yaml
./keykammer -keyfile photo.jpg

# Override config values with command line flags
./keykammer -keyfile photo.jpg -port 9999

# Without config file - uses built-in defaults
./keykammer -keyfile photo.jpg -port 53952
```

The included `keykammer.yaml` contains the standard default values with documentation. Simply edit this file to customize your default settings.

### Command Line Options

- `-keyfile path` - File to use as room key (required)
- `-discovery-server URL` - Discovery server address (default: https://discovery.keykammer.com)
- `-discovery-server-mode` - Run as discovery server
- `-connect IP:PORT` - Connect directly bypassing discovery
- `-port 53952` - Port for chat server (default: 53952)
- `-size 2` - Max users per room (0 = unlimited, default: 2)
- `-password "text"` - Optional password for additional entropy

### Room Status Indicators

- **LISTED** - Room is discoverable via discovery server and accepting connections
- **OPEN** - Room has space for direct connections but is not listed on a discovery server
- **CLOSED** - Room is at full capacity and not accepting any connections

### Privacy Features

- **Zero-logging discovery server** - No room IDs, user counts, or activity logged
- **Ephemeral rooms** - Rooms exist only in RAM, while users are connected
- **Automatic cleanup** - Rooms auto-delete when empty or at capacity
- **No persistent storage** - No room info or chat history (or anything else) saved to disk, ever

### Code Architecture

The codebase is organized into focused modules for maintainability:

- **`main.go`** - Application entry point and command-line argument parsing
- **`config.go`** - Constants, configuration structures, and application settings  
- **`crypto.go`** - All cryptographic functions including key derivation and AES-256-GCM encryption
- **`server.go`** - gRPC server implementation with user management, capacity control, and status broadcasting
- **`client.go`** - Client connection logic and chat session management
- **`discovery.go`** - Privacy-focused discovery server with zero-logging design
- **`tui.go`** - Terminal User Interface with chat panes, user list, and real-time updates
- **`upnp.go`** - UPnP port forwarding for automatic NAT traversal
- **`utils.go`** - Utility functions for file I/O and system checks
- **`messages.go`** - Message handling functions and legacy console interface

### Dependencies

- **Go** (obviously)
- **gRPC** (`google.golang.org/grpc`) - High-performance RPC framework
- **Protocol Buffers** (`google.golang.org/protobuf`) - Message serialization
- **HKDF** (`golang.org/x/crypto/hkdf`) - Key derivation function
- **TUI** (`github.com/rivo/tview`) - Terminal user interface library
- **UPnP** (`github.com/huin/goupnp`) - Automatic port forwarding
- **YAML** (`gopkg.in/yaml.v3`) - Configuration file parsing

### Building

**Be sure Go is installed:**
```
sudo apt install golang
```

**Using Make (recommended):**
```bash
# Install dependencies
make deps

# Build with version information
make build

# Build optimized release version
make release

# Quick development build
make dev
```

**Using build script:**
```bash
# Build with default version
./build.sh

# Build with custom version
./build.sh 2.0.0
```

**Manual build:**
```bash
# Install dependencies
go mod tidy

# Simple build (no version info)
go build -o keykammer *.go

# Run with keyfile
./keykammer -keyfile path/to/file

# Check version
./keykammer -version
```

## Current State

This is now a functional application. The encrypted chat functionality is working. It does still need more testing, and there are a few bugs to work out (mostly involving users force-closing at various states). Also, sometimes typed messages don't post to the chat, I'll work on that soon. I'd guess that the most urgent work is in getting users to connect without needing to explicitly mess with their port settings.

Pull requests welcome.

### Next?

- **Default discovery server** - I'll run a permanent public discovery service
- **General bug fixes** - Handle edge cases with force-closing and message posting
- **Connection improvements** - Easier networking without manual port/firewall configuration
- **Production features** - Docker support, maybe enable optional logging (on client-side)
- **Forward secrecy** - Keys could be modified based on chat content
- **File transfer** - Could send files through encrypted channels
- **Mobile clients** - This is the dream
