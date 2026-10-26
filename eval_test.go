package skills_test

import (
	"cmp"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"maragu.dev/gai/eval"
)

// TestEvalSkills discovers lore skills, gets baseline and skill-guided answers from the
// Responses API, judges them through the Decisions API, calculates summaries, and writes results (results-<timestamp>.yaml).
func TestEvalSkills(t *testing.T) {
	eval.Run(t, "skills", func(t *testing.T, _ *eval.E) {
		repoRoot := repoDir(t)
		loreDir := filepath.Join(repoRoot, "lore")
		skills := discoverSkillEvals(t, loreDir)
		if len(skills) == 0 {
			t.Fatalf("no lore skill questions found under %s", loreDir)
		}

		apiKey := os.Getenv("OPENAI_API_KEY")
		if apiKey == "" {
			t.Fatal("set OPENAI_API_KEY to run the eval")
		}

		responseModel := cmp.Or(os.Getenv("OPENAI_MODEL"), defaultModel)
		decisionModel := decisionsModel
		runner := &skillEvalRunner{
			repoRoot:      repoRoot,
			responseModel: responseModel,
			decisionModel: decisionModel,
			client: openAIClient{
				model:         responseModel,
				decisionModel: decisionModel,
				apiKey:        apiKey,
				httpClient:    http.DefaultClient,
			},
			usage: tokenUsageSummary{RecordType: "token_usage"},
		}
		t.Cleanup(func() { runner.writeResults(t) })
		runner.run(t, skills)
	})
}

func repoDir(t *testing.T) string {
	t.Helper()
	_, source, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("could not locate eval source file")
	}
	return filepath.Dir(source)
}
