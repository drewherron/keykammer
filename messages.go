package main

import (
	"bufio"
	"fmt"
	"os"
	"strings"
	"time"

	pb "keykammer/proto"
)

// sendMessage sends an encrypted chat message through the stream
func sendMessage(stream pb.KeykammerService_ChatClient, roomID, username, content string, key []byte) error {
	// Create encrypted message
	msg, err := createEncryptedMessage(roomID, username, content, key)
	if err != nil {
		return fmt.Errorf("failed to create encrypted message: %v", err)
	}

	return stream.Send(msg)
}

// handleIncomingMessages receives and displays decrypted messages from the chat stream (legacy console version)
func handleIncomingMessages(stream pb.KeykammerService_ChatClient, key []byte, done chan bool) {
	for {
		select {
		case <-done:
			return
		default:
			// Create a channel to receive the message with timeout
			msgChan := make(chan *pb.ChatMessage, 1)
			errChan := make(chan error, 1)

			go func() {
				msg, err := stream.Recv()
				if err != nil {
					errChan <- err
				} else {
					msgChan <- msg
				}
			}()

			// Wait for message or timeout
			select {
			case <-done:
				return
			case msg := <-msgChan:
				// Display the received message
				fmt.Print("\r") // Clear the input prompt
				displayChatMessage(msg, key)
				fmt.Print("> ") // Restore input prompt
			case err := <-errChan:
				// Check if we're supposed to stop
				select {
				case <-done:
					return
				default:
					if err.Error() != "EOF" {
						fmt.Printf("\nConnection error: %v\n", err)
						fmt.Print("> ") // Restore input prompt
					}
					return
				}
			case <-time.After(30 * time.Second):
				// Timeout - continue listening (this prevents blocking forever)
				continue
			}
		}
	}
}

// displayChatMessage formats and displays a decrypted chat message (legacy console version)
func displayChatMessage(msg *pb.ChatMessage, key []byte) {
	timestamp := time.Unix(0, msg.Timestamp).Format("15:04:05")

	// Decrypt the message content
	var content string
	if len(msg.EncryptedContent) > 0 {
		decrypted, err := decryptMessageContent(msg, key)
		if err != nil {
			content = fmt.Sprintf("[decryption error: %v]", err)
		} else {
			content = decrypted
		}
	} else {
		content = "[empty message]"
	}

	fmt.Printf("[%s] %s: %s\n", timestamp, msg.Username, content)
}

// handleUserInput processes user input and sends encrypted messages (legacy console version)
func handleUserInput(stream pb.KeykammerService_ChatClient, roomID, username string, key []byte, done chan bool) error {
	scanner := bufio.NewScanner(os.Stdin)

	for {
		fmt.Print("> ")
		if !scanner.Scan() {
			break
		}

		input := strings.TrimSpace(scanner.Text())

		// Handle empty input
		if input == "" {
			continue
		}

		// Handle quit command
		if input == "/quit" {
			fmt.Printf("Exiting chat...\n")
			close(done)
			// Give the message handler a moment to stop
			time.Sleep(100 * time.Millisecond)
			return nil
		}

		// Send the encrypted message
		err := sendMessage(stream, roomID, username, input, key)
		if err != nil {
			fmt.Printf("Failed to send message: %v\n", err)
			continue
		}
	}

	return nil
}
