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
		configDir, configErr := GetConfigDir()
		cacheDir, cacheErr := GetCacheDir()
		if configErr == nil {
			_ = os.Remove(filepath.Join(configDir, "config.json"))
		} else {
			fmt.Printf("Error resolving config directory: %v\n", configErr)
		}
		if cacheErr == nil {
			_ = os.Remove(filepath.Join(cacheDir, "token.json"))
		} else {
			fmt.Printf("Error resolving cache directory: %v\n", cacheErr)
		}
		if configErr == nil && cacheErr == nil {
			fmt.Println("Configuration and token files reset.")
		}
		os.Exit(0)
	}

	p := tea.NewProgram(
		initialModel(),
		tea.WithAltScreen(),
		tea.WithReportFocus(),
	)

	if _, err := p.Run(); err != nil {
		fmt.Printf("Uh oh, we encountered an error: %v\n", err)
		os.Exit(1)
	}
}
