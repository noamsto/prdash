package gh

import (
	"encoding/json"
	"strconv"
	"time"
)

type Comment struct {
	Author struct {
		Login string `json:"login"`
	} `json:"author"`
	Body      string    `json:"body"`
	CreatedAt time.Time `json:"createdAt"`
}

type Review struct {
	Author struct {
		Login string `json:"login"`
	} `json:"author"`
	Body        string    `json:"body"`
	State       string    `json:"state"`
	SubmittedAt time.Time `json:"submittedAt"`
}

type PRDetail struct {
	Comments []Comment `json:"comments"`
	Reviews  []Review  `json:"reviews"`
}

func PRViewArgs(number int) []string {
	return []string{"pr", "view", strconv.Itoa(number), "--json", "comments,reviews,latestReviews"}
}

func FetchPRDetail(r Runner, dir string, number int) (PRDetail, error) {
	out, err := r.Run(dir, PRViewArgs(number)...)
	if err != nil {
		return PRDetail{}, err
	}
	return ParsePRDetail(out)
}

func ParsePRDetail(b []byte) (PRDetail, error) {
	var d PRDetail
	err := json.Unmarshal(b, &d)
	return d, err
}
