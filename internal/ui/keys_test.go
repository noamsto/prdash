package ui

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
)

// ctrlC builds the ctrl+c key message the way the terminal actually delivers
// it, matching msg.String() == "ctrl+c".
func ctrlC() tea.KeyMsg {
	return tea.KeyPressMsg{Code: 'c', Mod: tea.ModCtrl}
}

func TestCtrlCQuitsFromEverySurface(t *testing.T) {
	if got := ctrlC().String(); got != "ctrl+c" {
		t.Fatalf("ctrlC().String() = %q, want ctrl+c", got)
	}

	tests := []struct {
		name  string
		setup func(t *testing.T) Model
	}{
		{
			name: "merge confirmation",
			setup: func(t *testing.T) Model {
				m := newTestModelWithRows(t)
				u, _ := m.Update(keyMsg("m"))
				m = u.(Model)
				if m.pending == nil {
					t.Fatal("expected 'm' to open the merge confirmation")
				}
				return m
			},
		},
		{
			name: "reviewer picker",
			setup: func(t *testing.T) Model {
				m := newTestModelWithRows(t)
				u, _ := m.Update(keyMsg("R"))
				m = u.(Model)
				if !m.showPicker || m.pickerMode != "reviewer" {
					t.Fatal("expected 'R' to open the reviewer picker")
				}
				return m
			},
		},
		{
			name: "actions palette",
			setup: func(t *testing.T) Model {
				m := newTestModelWithRows(t)
				u, _ := m.Update(keyMsg("a"))
				m = u.(Model)
				if !m.showActions {
					t.Fatal("expected 'a' to open the actions palette")
				}
				return m
			},
		},
		{
			name: "legend",
			setup: func(t *testing.T) Model {
				m := newTestModelWithRows(t)
				u, _ := m.Update(keyMsg("?"))
				m = u.(Model)
				if !m.showLegend {
					t.Fatal("expected '?' to open the legend")
				}
				return m
			},
		},
		{
			name: "filter bar on the PR board",
			setup: func(t *testing.T) Model {
				m := newTestModelWithRows(t)
				u, _ := m.Update(keyMsg("/"))
				m = u.(Model)
				if !m.filtering {
					t.Fatal("expected '/' to open the filter bar")
				}
				return m
			},
		},
		{
			name: "filter bar on the issue board",
			setup: func(t *testing.T) Model {
				m := newTestModelWithRows(t)
				u, _ := m.Update(keyMsg("tab"))
				m = u.(Model)
				u, _ = m.Update(keyMsg("/"))
				m = u.(Model)
				if m.mode != "issue" || !m.filtering {
					t.Fatal("expected tab then '/' to open the issue board's filter bar")
				}
				return m
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			m := tc.setup(t)
			_, cmd := m.Update(ctrlC())
			if cmd == nil {
				t.Fatal("ctrl+c returned a nil cmd, want tea.Quit")
			}
			if _, ok := cmd().(tea.QuitMsg); !ok {
				t.Fatal("ctrl+c did not quit")
			}
		})
	}
}

func TestIssueBoardPLeavesPreviewExpanded(t *testing.T) {
	m := newTestModelWithRows(t)
	u, _ := m.Update(keyMsg("tab"))
	m = u.(Model)
	if m.mode != "issue" {
		t.Fatal("expected tab to switch to the issue board")
	}
	u, _ = m.Update(keyMsg("p"))
	m = u.(Model)
	if m.previewExpanded {
		t.Fatal("'p' on the issue board must not expand the preview")
	}
}

func TestIssueBoardPROnlyKeysSetStatus(t *testing.T) {
	for _, key := range []string{"R", "D", "L"} {
		t.Run(key, func(t *testing.T) {
			m := newTestModelWithRows(t)
			u, _ := m.Update(keyMsg("tab"))
			m = u.(Model)
			if m.mode != "issue" {
				t.Fatal("expected tab to switch to the issue board")
			}
			u, _ = m.Update(keyMsg(key))
			m = u.(Model)
			if m.actionStatus == nil {
				t.Fatalf("expected %q to set an action status on the issue board", key)
			}
			// "(R)", not "R": "PR-only" itself contains an R.
			if want := "is PR-only (" + key + ")"; !strings.HasSuffix(m.actionStatus.fail, want) {
				t.Fatalf("actionStatus.fail = %q, want suffix %q", m.actionStatus.fail, want)
			}
		})
	}
}
