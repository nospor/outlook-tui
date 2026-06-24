package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"

	tea "github.com/charmbracelet/bubbletea"
)

func main() {
	resetFlag := flag.Bool("reset", false, "Reset configuration and login token")
	flag.Parse()

	if *resetFlag {
		configDir, err := GetConfigDir()
		if err == nil {
			configPath := filepath.Join(configDir, "config.json")
			tokenPath := filepath.Join(configDir, "token.json")
			_ = os.Remove(configPath)
			_ = os.Remove(tokenPath)
			fmt.Println("Configuration and token files reset.")
		} else {
			fmt.Printf("Error resolving config directory: %v\n", err)
		}
		os.Exit(0)
	}

	p := tea.NewProgram(
		initialModel(),
		tea.WithAltScreen(),       // Use alt screen
		tea.WithMouseCellMotion(), // Support mouse interactions/scrolling
	)

	if _, err := p.Run(); err != nil {
		fmt.Printf("Uh oh, we encountered an error: %v\n", err)
		os.Exit(1)
	}
}
