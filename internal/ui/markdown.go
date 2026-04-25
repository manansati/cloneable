package ui

import (
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// ── Markdown rendering styles ─────────────────────────────────────────────────

var (
	mdH1 = lipgloss.NewStyle().
		Foreground(ColorSaffron).
		Bold(true).
		MarginTop(1).
		MarginBottom(1).
		BorderBottom(true).
		BorderStyle(lipgloss.NormalBorder()).
		BorderForeground(ColorSaffronDark)

	mdH2 = lipgloss.NewStyle().
		Foreground(ColorSaffronLight).
		Bold(true).
		MarginTop(1)

	mdH3 = lipgloss.NewStyle().
		Foreground(ColorBlue).
		Bold(true)

	mdH4 = lipgloss.NewStyle().
		Foreground(ColorGray).
		Bold(true)

	mdBold = lipgloss.NewStyle().
		Bold(true).
		Foreground(ColorOffWhite)

	mdItalic = lipgloss.NewStyle().
			Italic(true).
			Foreground(ColorGray)

	mdCode = lipgloss.NewStyle().
		Foreground(ColorSaffronLight).
		Background(lipgloss.Color("#1E1E1E"))

	mdCodeBlock = lipgloss.NewStyle().
			Foreground(ColorSaffronLight).
			Background(lipgloss.Color("#1A1A1A")).
			Padding(0, 1)

	mdLink = lipgloss.NewStyle().
		Foreground(ColorBlue).
		Underline(true)

	mdLinkURL = lipgloss.NewStyle().
			Foreground(ColorDarkGray)

	mdBlockquote = lipgloss.NewStyle().
			Foreground(ColorGray).
			Italic(true).
			PaddingLeft(2).
			BorderLeft(true).
			BorderStyle(lipgloss.NormalBorder()).
			BorderForeground(ColorSaffronDark)

	mdListBullet = lipgloss.NewStyle().
			Foreground(ColorSaffron)

	mdHR = lipgloss.NewStyle().
		Foreground(ColorDarkGray)

	mdTable = lipgloss.NewStyle().
		Foreground(ColorOffWhite)

	mdTableHeader = lipgloss.NewStyle().
			Foreground(ColorSaffronLight).
			Bold(true)
)

// RenderMarkdown renders a markdown file beautifully in the terminal.
// It's a built-in alternative to glow/mdcat — no external tools needed.
// Output is piped to a pager (less -R) if the content is long enough.
func RenderMarkdown(filePath string) error {
	data, err := os.ReadFile(filePath)
	if err != nil {
		return fmt.Errorf("could not read %s: %w", filePath, err)
	}

	rendered := renderMarkdownString(string(data))

	// Count lines to decide whether to use a pager
	lineCount := strings.Count(rendered, "\n")
	termHeight := 40 // safe default

	if lineCount > termHeight {
		return pipeToLess(rendered)
	}

	fmt.Println(rendered)
	return nil
}

// renderMarkdownString converts markdown text to ANSI-styled terminal output.
func renderMarkdownString(content string) string {
	lines := strings.Split(content, "\n")
	var out []string
	inCodeBlock := false
	var codeBlockLines []string
	codeBlockLang := ""
	inTable := false

	for i := 0; i < len(lines); i++ {
		line := lines[i]

		// ── Code blocks ──────────────────────────────────────────────
		if strings.HasPrefix(line, "```") {
			if inCodeBlock {
				// End code block — render accumulated lines
				codeContent := strings.Join(codeBlockLines, "\n")
				if codeBlockLang != "" {
					out = append(out, fmt.Sprintf("  %s", StyleMuted.Render("── "+codeBlockLang+" "+"─────────────")))
				}
				for _, cl := range strings.Split(codeContent, "\n") {
					out = append(out, "  "+mdCodeBlock.Render(cl))
				}
				if codeBlockLang != "" {
					out = append(out, fmt.Sprintf("  %s", StyleMuted.Render("──────────────────────")))
				}
				codeBlockLines = nil
				codeBlockLang = ""
				inCodeBlock = false
			} else {
				inCodeBlock = true
				codeBlockLang = strings.TrimPrefix(line, "```")
				codeBlockLang = strings.TrimSpace(codeBlockLang)
			}
			continue
		}

		if inCodeBlock {
			codeBlockLines = append(codeBlockLines, line)
			continue
		}

		// ── Horizontal rule ──────────────────────────────────────────
		trimmed := strings.TrimSpace(line)
		if isHorizontalRule(trimmed) {
			out = append(out, mdHR.Render("  ────────────────────────────────────────"))
			continue
		}

		// ── Headers ──────────────────────────────────────────────────
		if strings.HasPrefix(trimmed, "# ") {
			text := strings.TrimPrefix(trimmed, "# ")
			out = append(out, "")
			out = append(out, "  "+mdH1.Render("  "+text+"  "))
			out = append(out, "")
			continue
		}
		if strings.HasPrefix(trimmed, "## ") {
			text := strings.TrimPrefix(trimmed, "## ")
			out = append(out, "")
			out = append(out, "  "+mdH2.Render("▎ "+text))
			continue
		}
		if strings.HasPrefix(trimmed, "### ") {
			text := strings.TrimPrefix(trimmed, "### ")
			out = append(out, "  "+mdH3.Render("  "+text))
			continue
		}
		if strings.HasPrefix(trimmed, "#### ") {
			text := strings.TrimPrefix(trimmed, "#### ")
			out = append(out, "  "+mdH4.Render("  "+text))
			continue
		}

		// ── Blockquote ───────────────────────────────────────────────
		if strings.HasPrefix(trimmed, "> ") {
			text := strings.TrimPrefix(trimmed, "> ")
			out = append(out, "  "+mdBlockquote.Render(text))
			continue
		}

		// ── Tables ───────────────────────────────────────────────────
		if strings.Contains(trimmed, "|") && strings.Count(trimmed, "|") >= 2 {
			// Check if this is a table separator line
			if isTableSeparator(trimmed) {
				continue // skip separator lines
			}
			cells := parseTableRow(trimmed)
			if !inTable {
				// First row = header
				inTable = true
				var headerCells []string
				for _, cell := range cells {
					headerCells = append(headerCells, mdTableHeader.Render(cell))
				}
				out = append(out, "  "+strings.Join(headerCells, mdTable.Render(" │ ")))
				out = append(out, "  "+StyleMuted.Render(strings.Repeat("─", 60)))
			} else {
				var dataCells []string
				for _, cell := range cells {
					dataCells = append(dataCells, mdTable.Render(cell))
				}
				out = append(out, "  "+strings.Join(dataCells, mdTable.Render(" │ ")))
			}
			continue
		} else if inTable {
			inTable = false
		}

		// ── List items ───────────────────────────────────────────────
		if strings.HasPrefix(trimmed, "- ") || strings.HasPrefix(trimmed, "* ") {
			text := trimmed[2:]
			indent := countLeadingSpaces(line) / 2
			prefix := strings.Repeat("  ", indent)
			bullet := mdListBullet.Render("●")
			out = append(out, fmt.Sprintf("  %s %s %s", prefix, bullet, renderInline(text)))
			continue
		}
		// Numbered lists
		if isNumberedList(trimmed) {
			idx := strings.Index(trimmed, ".")
			if idx > 0 {
				num := trimmed[:idx]
				text := strings.TrimSpace(trimmed[idx+1:])
				out = append(out, fmt.Sprintf("  %s %s", mdListBullet.Render(num+"."), renderInline(text)))
				continue
			}
		}

		// ── HTML comments — skip entirely ────────────────────────────
		if strings.HasPrefix(trimmed, "<!--") {
			// Skip multi-line comments
			if !strings.Contains(trimmed, "-->") {
				for i++; i < len(lines); i++ {
					if strings.Contains(lines[i], "-->") {
						break
					}
				}
			}
			continue
		}

		// ── HTML tags — strip and render inner text ──────────────────
		if strings.HasPrefix(trimmed, "<") && strings.Contains(trimmed, ">") {
			// Strip all HTML tags, render any remaining text
			stripped := stripHTMLTags(trimmed)
			if strings.TrimSpace(stripped) != "" {
				out = append(out, "  "+renderInline(strings.TrimSpace(stripped)))
			}
			continue
		}

		// ── Images ![alt](url) — show alt text ──────────────────────
		if strings.HasPrefix(trimmed, "![") {
			closeBracket := strings.Index(trimmed, "](")
			if closeBracket > 0 {
				altText := trimmed[2:closeBracket]
				out = append(out, "  "+mdItalic.Render("[image: "+altText+"]"))
				continue
			}
		}

		// ── Empty lines ──────────────────────────────────────────────
		if trimmed == "" {
			out = append(out, "")
			continue
		}

		// ── Normal paragraph text ────────────────────────────────────
		out = append(out, "  "+renderInline(trimmed))
	}

	return strings.Join(out, "\n")
}

// renderInline processes inline markdown: **bold**, *italic*, `code`, [links](url)
func renderInline(text string) string {
	result := text

	// Process inline code first (before bold/italic to avoid conflicts)
	result = processInlineCode(result)

	// Process bold: **text**
	result = processDelimited(result, "**", func(s string) string {
		return mdBold.Render(s)
	})

	// Process italic: *text* (but not inside **)
	result = processDelimited(result, "*", func(s string) string {
		return mdItalic.Render(s)
	})

	// Process links: [text](url)
	result = processLinks(result)

	return result
}

// processInlineCode replaces `code` with styled code.
func processInlineCode(text string) string {
	var result strings.Builder
	parts := strings.Split(text, "`")
	for i, part := range parts {
		if i%2 == 1 {
			// Inside backticks
			result.WriteString(mdCode.Render(" " + part + " "))
		} else {
			result.WriteString(part)
		}
	}
	return result.String()
}

// processDelimited finds text between delimiter pairs and applies a style.
func processDelimited(text, delim string, style func(string) string) string {
	offset := 0
	for {
		start := strings.Index(text[offset:], delim)
		if start < 0 {
			break
		}
		start += offset

		end := strings.Index(text[start+len(delim):], delim)
		if end < 0 {
			offset = start + len(delim)
			continue
		}
		end += start + len(delim)

		inner := text[start+len(delim) : end]
		styled := style(inner)
		text = text[:start] + styled + text[end+len(delim):]
		offset = start + len(styled)
	}
	return text
}

// processLinks converts [text](url) into styled links.
func processLinks(text string) string {
	offset := 0
	for {
		openBracket := strings.Index(text[offset:], "[")
		if openBracket < 0 {
			break
		}
		openBracket += offset

		closeBracket := strings.Index(text[openBracket:], "](")
		if closeBracket < 0 {
			offset = openBracket + 1
			continue
		}
		closeBracket += openBracket

		closeParen := strings.Index(text[closeBracket+2:], ")")
		if closeParen < 0 {
			offset = openBracket + 1
			continue
		}
		closeParen += closeBracket + 2

		linkText := text[openBracket+1 : closeBracket]
		linkURL := text[closeBracket+2 : closeParen]

		styled := mdLink.Render(linkText) + " " + mdLinkURL.Render("("+linkURL+")")
		text = text[:openBracket] + styled + text[closeParen+1:]
		offset = openBracket + len(styled)
	}
	return text
}

// ── small helpers ─────────────────────────────────────────────────────────────

func isHorizontalRule(line string) bool {
	clean := strings.ReplaceAll(line, " ", "")
	if len(clean) < 3 {
		return false
	}
	return strings.Trim(clean, "-") == "" ||
		strings.Trim(clean, "*") == "" ||
		strings.Trim(clean, "_") == ""
}

func isTableSeparator(line string) bool {
	clean := strings.ReplaceAll(line, "|", "")
	clean = strings.ReplaceAll(clean, "-", "")
	clean = strings.ReplaceAll(clean, ":", "")
	clean = strings.TrimSpace(clean)
	return clean == ""
}

func parseTableRow(line string) []string {
	line = strings.TrimSpace(line)
	line = strings.TrimPrefix(line, "|")
	line = strings.TrimSuffix(line, "|")
	parts := strings.Split(line, "|")
	var cells []string
	for _, p := range parts {
		cells = append(cells, strings.TrimSpace(p))
	}
	return cells
}

func countLeadingSpaces(s string) int {
	count := 0
	for _, ch := range s {
		if ch == ' ' {
			count++
		} else if ch == '\t' {
			count += 2
		} else {
			break
		}
	}
	return count
}

func isNumberedList(s string) bool {
	for i, ch := range s {
		if ch >= '0' && ch <= '9' {
			continue
		}
		if ch == '.' && i > 0 {
			return true
		}
		break
	}
	return false
}

// pipeToLess sends content to `less -R` for paged viewing with ANSI colors.
// Falls back to `more` if less isn't available, and to direct output as last resort.
func pipeToLess(content string) error {
	pager := "less"
	args := []string{"-R", "-S", "--prompt= Use ↑↓ to scroll, q to quit"}

	if _, err := exec.LookPath("less"); err != nil {
		if _, err := exec.LookPath("more"); err != nil {
			// No pager — just print
			fmt.Println(content)
			return nil
		}
		pager = "more"
		args = nil
	}

	cmd := exec.Command(pager, args...)
	cmd.Stdin = strings.NewReader(content)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

// stripHTMLTags removes all HTML tags from a string, preserving inner text.
// e.g. "<p>hello <b>world</b></p>" → "hello world"
func stripHTMLTags(s string) string {
	var result strings.Builder
	inTag := false
	for _, ch := range s {
		if ch == '<' {
			inTag = true
			continue
		}
		if ch == '>' {
			inTag = false
			continue
		}
		if !inTag {
			result.WriteRune(ch)
		}
	}
	return result.String()
}
