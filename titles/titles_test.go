package titles

import "testing"

func TestEveryTitleIsComplete(t *testing.T) {
	for name, title := range All {
		if title.Name != name || title.Listen == "" || title.Run == nil {
			t.Errorf("%s: Name, Listen and Run are all required", name)
		}
	}
}
