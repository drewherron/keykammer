package main

import (
	"fmt"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
	pb "keykammer/proto"
)

// TUI global variables
var (
	app      *tview.Application
	chatView *tview.TextView
	userList *tview.List
	inputField *tview.InputField
	mainFlex *tview.Flex
	currentUsers []string // Track current user list for TUI
	usersMutex sync.RWMutex // Protect currentUsers access
	messageCount int // Track number of messages in chat view
	messageMutex sync.Mutex // Protect message count access
)

// setupTUI initializes the TUI layout with chat, user list, and input panes
func setupTUI(roomID, username string) error {
	app = tview.NewApplication()
	
	// Set application to use terminal default colors
	tview.Styles.PrimitiveBackgroundColor = tcell.ColorDefault
	tview.Styles.ContrastBackgroundColor = tcell.ColorDefault
	tview.Styles.MoreContrastBackgroundColor = tcell.ColorDefault
	tview.Styles.BorderColor = tcell.ColorDefault
	tview.Styles.TitleColor = tcell.ColorDefault
	tview.Styles.GraphicsColor = tcell.ColorDefault
	tview.Styles.PrimaryTextColor = tcell.ColorDefault
	tview.Styles.SecondaryTextColor = tcell.ColorDefault
	tview.Styles.TertiaryTextColor = tcell.ColorDefault
	tview.Styles.InverseTextColor = tcell.ColorDefault
	
	// Create chat view (main pane)
	chatView = tview.NewTextView()
	chatView.SetBorder(true).SetTitle("Keykammer - OPEN")
	chatView.SetScrollable(true)
	chatView.SetWrap(true)
	chatView.SetDynamicColors(false)
	chatView.SetTextColor(tcell.ColorDefault)
	chatView.SetBackgroundColor(tcell.ColorDefault)
	
	// Create user list (right pane)
	userList = tview.NewList()
	userList.SetBorder(true).SetTitle("Users")
	userList.ShowSecondaryText(false)
	userList.SetMainTextColor(tcell.ColorDefault)
	userList.SetBackgroundColor(tcell.ColorDefault)
	
	// Create input field (bottom pane)
	inputField = tview.NewInputField()
	inputField.SetBorder(true).SetTitle("")
	inputField.SetLabel(fmt.Sprintf("%s: ", username))
	inputField.SetFieldTextColor(tcell.ColorDefault)
	inputField.SetFieldBackgroundColor(tcell.ColorDefault)
	
	// Create layout with right sidebar for users and bottom input
	rightFlex := tview.NewFlex().SetDirection(tview.FlexRow)
	rightFlex.AddItem(userList, 0, 1, false)
	
	bottomFlex := tview.NewFlex().SetDirection(tview.FlexColumn)
	bottomFlex.AddItem(inputField, 0, 1, true)
	
	mainFlex = tview.NewFlex().SetDirection(tview.FlexRow)
	
	topFlex := tview.NewFlex().SetDirection(tview.FlexColumn)
	topFlex.AddItem(chatView, 0, 3, false)     // Chat takes 3/4 of width
	topFlex.AddItem(rightFlex, 0, 1, false)   // User list takes 1/4 of width
	
	mainFlex.AddItem(topFlex, 0, 4, false)    // Top section takes 4/5 of height
	mainFlex.AddItem(bottomFlex, 3, 0, true)  // Input takes 3 lines at bottom
	
	// Set up keyboard shortcuts and navigation
	app.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
		switch event.Key() {
		case tcell.KeyCtrlC:
			// Ctrl+C to quit
			app.Stop()
			return nil
		case tcell.KeyTab:
			// Tab to cycle focus between panes
			currentFocus := app.GetFocus()
			if currentFocus == inputField {
				app.SetFocus(chatView)
			} else if currentFocus == chatView {
				app.SetFocus(userList)
			} else {
				app.SetFocus(inputField)
			}
			return nil
		case tcell.KeyEsc:
			// Escape to return focus to input field
			app.SetFocus(inputField)
			return nil
		}
		return event
	})
	
	app.SetRoot(mainFlex, true)
	app.SetFocus(inputField)
	
	// Add initial welcome messages directly to chat view
	timestamp := time.Now().Format("15:04:05")
	fmt.Fprintf(chatView, "[%s] System: Successfully joined room %s as %s\n", timestamp, formatRoomID(roomID), username)
	fmt.Fprintf(chatView, "[%s] System: Commands: /quit to exit, /help for help\n", timestamp)
	fmt.Fprintf(chatView, "[%s] System: Use Tab to navigate between panes, Esc to return to input\n", timestamp)
	messageCount = 3 // Track the messages we just added
	
	// Initialize user list directly with current user
	userList.AddItem(username, "", 0, nil)
	userList.SetTitle("Users (1)")
	usersMutex.Lock()
	currentUsers = []string{username}
	usersMutex.Unlock()
	
	return nil
}

// updateRoomStatus updates the chat title to show OPEN/CLOSED status
func updateRoomStatus(isOpen bool) {
	if chatView == nil {
		return
	}
	
	status := "CLOSED"
	if isOpen {
		status = "OPEN"
	}
	
	app.QueueUpdateDraw(func() {
		chatView.SetTitle(fmt.Sprintf("Keykammer - %s", status))
	})
}

// addChatMessage adds a message to the chat pane with history management
func addChatMessage(username, message string) {
	if chatView == nil {
		return
	}
	
	timestamp := time.Now().Format("15:04:05")
	formattedMsg := fmt.Sprintf("[%s] %s: %s\n", timestamp, username, message)
	
	app.QueueUpdateDraw(func() {
		messageMutex.Lock()
		defer messageMutex.Unlock()
		
		// Check if we need to trim chat history
		if messageCount >= MaxChatHistory {
			// Clear the chat view and reset counter
			chatView.Clear()
			messageCount = 0
			// Add a notice that history was cleared
			fmt.Fprint(chatView, "--- Chat history cleared to save memory ---\n")
			messageCount++
		}
		
		fmt.Fprint(chatView, formattedMsg)
		messageCount++
		chatView.ScrollToEnd()
	})
}

// updateUserList updates the user list pane with current users
func updateUserList(users []string) {
	if userList == nil {
		return
	}
	
	usersMutex.Lock()
	currentUsers = make([]string, len(users))
	copy(currentUsers, users)
	usersMutex.Unlock()
	
	app.QueueUpdateDraw(func() {
		userList.Clear()
		for _, user := range users {
			userList.AddItem(user, "", 0, nil)
		}
		userList.SetTitle(fmt.Sprintf("Users (%d)", len(users)))
	})
}

// handleIncomingMessagesTUI receives and displays decrypted messages in the TUI chat pane
func handleIncomingMessagesTUI(stream pb.KeykammerService_ChatClient, key []byte, done chan bool) {
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
				// Display the received message in TUI
				displayChatMessageTUI(msg, key)
			case err := <-errChan:
				// Check if we're supposed to stop
				select {
				case <-done:
					return
				default:
					if err.Error() != "EOF" {
						addChatMessage("System", fmt.Sprintf("Connection error: %v", err))
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

// displayChatMessageTUI displays a decrypted message in the TUI chat pane
func displayChatMessageTUI(msg *pb.ChatMessage, key []byte) {
	// Handle system messages that don't need decryption
	if msg.Username == "System" && len(msg.EncryptedContent) > 0 {
		content := string(msg.EncryptedContent)
		
		// Check if this is a user list update message
		if strings.HasPrefix(content, "USERLIST:") {
			userListData := strings.TrimPrefix(content, "USERLIST:")
			if userListData == "" {
				updateUserList([]string{})
			} else {
				users := strings.Split(userListData, ",")
				updateUserList(users)
			}
			return // Don't display user list updates as chat messages
		}
		
		addChatMessage(msg.Username, content)
		return
	}
	
	// Decrypt the message content for regular messages
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
	
	addChatMessage(msg.Username, content)
}

// setupTUIInputHandling configures input field to send messages
func setupTUIInputHandling(stream pb.KeykammerService_ChatClient, roomID, username string, key []byte, done chan bool) {
	inputField.SetDoneFunc(func(keyPressed tcell.Key) {
		if keyPressed == tcell.KeyCtrlD {
			// Ctrl+D pressed - exit program entirely
			if globalCleanupFunc != nil {
				fmt.Printf("\nCtrl+D pressed. Cleaning up...\n")
				globalCleanupFunc()
			}
			fmt.Printf("Exiting...\n")
			os.Exit(0)
			return
		}
		if keyPressed == tcell.KeyEnter {
			input := strings.TrimSpace(inputField.GetText())
			inputField.SetText("")
			
			// Handle empty input
			if input == "" {
				return
			}
			
			// Handle quit command
			if input == "/quit" {
				go func() {
					close(done)
				}()
				app.Stop()
				return
			}
			
			// Handle help command
			if input == "/help" {
				go func() {
					addChatMessage("System", "Available commands:")
					addChatMessage("System", "  /quit - Exit the chat")
					addChatMessage("System", "  /help - Show this help message")
					addChatMessage("System", "Keyboard shortcuts:")
					addChatMessage("System", "  Tab - Cycle between panes (arrow keys to scroll)")
					addChatMessage("System", "  Esc - Return to input field")
				}()
				return
			}
			
			// Send the message
			err := sendMessage(stream, roomID, username, input, key)
			if err != nil {
				addChatMessage("System", fmt.Sprintf("Failed to send message: %v", err))
			}
		}
	})
}

// displayMessage formats and displays a chat message with username (legacy function for compatibility)
func displayMessage(username, message string) {
	timestamp := time.Now().Format("15:04:05")
	fmt.Printf("[%s] %s: %s\n", timestamp, username, message)
}