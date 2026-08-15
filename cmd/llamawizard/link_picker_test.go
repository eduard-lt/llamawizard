package main

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

// Guards the "stuck download" regression: after a progress message, Update
// must return a command that re-arms the listener. (Previously the channel was
// stored on a value-receiver copy, so re-arming read from a nil channel and
// progress froze after the first message.)
func TestLinkPickerReArmsProgress(t *testing.T) {
	m := &linkPicker{
		screen:  lsDownload,
		total:   100,
		bar:     newLinkBar(),
		progCh:  make(chan tea.Msg, 1),
		current: "model.gguf",
	}

	_, cmd := m.Update(linkDlMsg{downloaded: 10, total: 100})
	if cmd == nil {
		t.Fatal("expected a command that re-arms the progress listener, got nil")
	}
}

func TestLinkDlListen_ReturnsMessage(t *testing.T) {
	ch := make(chan tea.Msg, 1)
	ch <- linkDlMsg{downloaded: 42}

	msg := linkDlListen(ch)()
	dm, ok := msg.(linkDlMsg)
	if !ok {
		t.Fatalf("expected linkDlMsg, got %T", msg)
	}
	if dm.downloaded != 42 {
		t.Errorf("downloaded = %d, want 42", dm.downloaded)
	}
}

func TestLinkDlListen_ClosedChannelReturnsDone(t *testing.T) {
	ch := make(chan tea.Msg)
	close(ch)

	msg := linkDlListen(ch)()
	dm, ok := msg.(linkDlMsg)
	if !ok {
		t.Fatalf("expected linkDlMsg, got %T", msg)
	}
	if !dm.done {
		t.Error("expected done message on closed channel")
	}
}
