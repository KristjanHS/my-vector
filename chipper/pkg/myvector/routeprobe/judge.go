package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/kercre123/wire-pod/chipper/pkg/myvector/probekit"
)

const judgeRubric = `You grade one reply from Vector, a small home robot that answers spoken questions out loud.
Score it from 0 to 100 on all of these together:
- it answers the ask;
- it is correct;
- it is 1-3 spoken sentences (a story or step-by-step ask may run a little longer);
- it is in the same language as the ask;
- it has no markdown: no lists, headings, asterisks or code.
Reply with only JSON: {"score": N}`

// judge is the rubric score of reply to a.
func judge(key, model string, a ask, reply string) (int, error) {
	user := fmt.Sprintf("Language: %s\nAsk: %s\nReply: %s", a.Lang, a.Text, reply)
	out, err := probekit.Claude(key, model, judgeRubric, user, 200)
	if err != nil {
		return 0, err
	}
	return parseJudge(out)
}

// parseJudge is the score in the first {…} span of reply (fences and prose around it ignored).
func parseJudge(reply string) (int, error) {
	i, j := strings.IndexByte(reply, '{'), strings.LastIndexByte(reply, '}')
	if i < 0 || j < i {
		return 0, fmt.Errorf("no JSON object in %q", reply)
	}
	var s struct {
		Score *int `json:"score"`
	}
	if err := json.Unmarshal([]byte(reply[i:j+1]), &s); err != nil {
		return 0, err
	}
	if s.Score == nil {
		return 0, errors.New("score required")
	}
	if *s.Score < 0 || *s.Score > 100 {
		return 0, fmt.Errorf("score %d out of 0..100", *s.Score)
	}
	return *s.Score, nil
}
