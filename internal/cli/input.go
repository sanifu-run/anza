package cli

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"regexp"
	"strings"
	"unicode"
)

var ansiControl = regexp.MustCompile("\\x1b\\[[0-?]*[ -/]*[@-~]")

// lineInput keeps terminal interaction line-oriented so the wizard also works
// in scripts and plain terminals without cursor or color support.
type lineInput struct {
	reader *bufio.Reader
	writer io.Writer
}

func newLineInput(reader io.Reader, writer io.Writer) *lineInput {
	return &lineInput{reader: bufio.NewReader(reader), writer: writer}
}

func (in *lineInput) ask(prompt string) (string, error) {
	if _, err := fmt.Fprint(in.writer, prompt); err != nil {
		return "", fmt.Errorf("write prompt: %w", err)
	}
	line, err := in.reader.ReadString('\n')
	if err != nil && !errors.Is(err, io.EOF) {
		return "", fmt.Errorf("read response: %w", err)
	}
	line = strings.TrimSuffix(strings.TrimSuffix(line, "\n"), "\r")
	if errors.Is(err, io.EOF) && line == "" {
		return "", io.EOF
	}
	return line, nil
}

func terminalText(value string) string {
	value = ansiControl.ReplaceAllString(value, "")
	var clean strings.Builder
	for _, r := range value {
		if r == '\n' || r == '\t' || !unicode.IsControl(r) {
			clean.WriteRune(r)
		}
	}
	return clean.String()
}
