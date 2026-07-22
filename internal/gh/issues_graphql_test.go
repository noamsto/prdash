package gh

import (
	"testing"
	"time"

	"github.com/shurcooL/githubv4"
)

func TestMapIssueBotAuthorPrefix(t *testing.T) {
	var bot qlIssue
	bot.Author.Login = "dependabot"
	bot.Author.Typename = "Bot"
	if got := mapIssue(bot).Author.Login; got != "app/dependabot" {
		t.Errorf("bot author = %q, want app/dependabot (gh's app/ prefix)", got)
	}

	var human qlIssue
	human.Author.Login = "octocat"
	human.Author.Typename = "User"
	if got := mapIssue(human).Author.Login; got != "octocat" {
		t.Errorf("user author = %q, want octocat (no prefix)", got)
	}
}

func TestMapIssueLabelsAndAssignees(t *testing.T) {
	var g qlIssue
	g.Labels.Nodes = []struct{ Name, Color string }{
		{Name: "bug", Color: "d73a4a"},
		{Name: "help wanted", Color: "008672"},
	}
	g.Assignees.Nodes = []struct{ Login string }{
		{Login: "alice"}, {Login: "bob"},
	}

	i := mapIssue(g)

	if len(i.Labels) != 2 || i.Labels[0].Name != "bug" || i.Labels[0].Color != "d73a4a" ||
		i.Labels[1].Name != "help wanted" || i.Labels[1].Color != "008672" {
		t.Errorf("labels = %+v, want [bug/d73a4a, help wanted/008672]", i.Labels)
	}
	if len(i.Assignees) != 2 || i.Assignees[0].Login != "alice" || i.Assignees[1].Login != "bob" {
		t.Errorf("assignees = %+v, want [alice, bob]", i.Assignees)
	}
}

func TestMapIssueScalarFields(t *testing.T) {
	g := qlIssue{
		Number:    42,
		Title:     "something broke",
		URL:       "https://github.com/o/r/issues/42",
		UpdatedAt: githubv4.DateTime{Time: time.Unix(100, 0)},
	}
	i := mapIssue(g)
	if i.Number != 42 || i.Title != "something broke" || i.URL != "https://github.com/o/r/issues/42" {
		t.Errorf("scalars = %+v, want number=42 title=%q url=%q", i, g.Title, g.URL)
	}
	if !i.UpdatedAt.Equal(time.Unix(100, 0)) {
		t.Errorf("UpdatedAt = %v, want %v", i.UpdatedAt, time.Unix(100, 0))
	}
}
