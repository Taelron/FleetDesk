package app

import "github.com/charmbracelet/lipgloss"

var (
	// colors
	colorBlue   = lipgloss.Color("33")
	colorCyan   = lipgloss.Color("86")
	colorGreen  = lipgloss.Color("42")
	colorRed    = lipgloss.Color("196")
	colorYellow = lipgloss.Color("220")
	colorDimmed = lipgloss.Color("241")
	colorWhite  = lipgloss.Color("255")
	colorBorder = lipgloss.Color("238")

	// header bar
	headerStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(colorBlue).
			Background(lipgloss.Color("235")).
			Padding(0, 1)

	headerCountStyle = lipgloss.NewStyle().
				Foreground(colorDimmed).
				Background(lipgloss.Color("235")).
				Padding(0, 1)

	// table column headers
	colHeaderStyle = lipgloss.NewStyle().
			Foreground(colorBlue).
			Bold(true).
			Background(lipgloss.Color("238"))

	// table rows
	selectedRowStyle = lipgloss.NewStyle().
				Foreground(colorWhite).
				Bold(true).
				Background(lipgloss.Color("24"))

	normalRowStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("250"))

	altRowStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("250")).
			Background(lipgloss.Color("234"))

	// borders
	borderStyle = lipgloss.NewStyle().
			Foreground(colorBorder)

	// hint bar
	hintKeyStyle = lipgloss.NewStyle().
			Foreground(colorBlue).
			Bold(true)

	hintActionStyle = lipgloss.NewStyle().
			Foreground(colorDimmed)

	hintBarStyle = lipgloss.NewStyle().
			Background(lipgloss.Color("235")).
			Padding(0, 1)

	flashStyle = lipgloss.NewStyle().
			Foreground(colorGreen).
			Bold(true)

	flashErrorStyle = lipgloss.NewStyle().
			Foreground(colorRed).
			Bold(true)

	groupHeaderStyle = lipgloss.NewStyle().
				Foreground(colorBlue).
				Bold(true)

	// modal overlay
	modalTitleStyle = lipgloss.NewStyle().
			Foreground(colorBlue).
			Bold(true)

	modalDimStyle = lipgloss.NewStyle().
			Foreground(colorDimmed)

	modalKeyStyle = lipgloss.NewStyle().
			Foreground(colorBlue).
			Bold(true)

	modalInputStyle = lipgloss.NewStyle().
			Foreground(colorWhite).
			Background(lipgloss.Color("236")).
			Padding(0, 1)

	modalErrorStyle = lipgloss.NewStyle().
			Foreground(colorRed).
			Bold(true)
)
