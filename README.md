# Keykammer

**UNDER CONSTRUCTION**

This project is just beginning. Eventually, it will be a (mostly) P2P encrypted chatroom using gRPC. Any file can be supplied to the program, and this is used both as the encryption key and to derive a unique room ID. If another user (anywhere) starts the program with the same file, they will be connected to the same chatroom. Without the file, there is no way to connect to the room, or to decrypt the messages, or to even know that the room exists.
