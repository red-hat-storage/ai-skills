package skills_test

import (
	"os"
	"path/filepath"
	"testing"

	"sigs.k8s.io/yaml"
)

type question struct {
	ID       string `json:"id"`
	Polarity string `json:"polarity"`
	Question string `json:"question"`
	Expected string `json:"expected"`
}

type skillEval struct {
	name        string
	instruction string
	questions   []question
}

func discoverSkillEvals(t *testing.T, root string) []skillEval {
	t.Helper()

	entries, err := os.ReadDir(root)
	if err != nil {
		t.Fatal(err)
	}

	var skills []skillEval
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		dir := filepath.Join(root, entry.Name())
		skillPath := filepath.Join(dir, "SKILL.md")
		questionsPath := filepath.Join(dir, "questions.yaml")
		_, skillErr := os.Stat(skillPath)
		_, questionsErr := os.Stat(questionsPath)
		if os.IsNotExist(skillErr) && os.IsNotExist(questionsErr) {
			continue
		}
		if os.IsNotExist(skillErr) {
			t.Fatalf("lore skill %s has questions.yaml but is missing SKILL.md", entry.Name())
		}
		if skillErr != nil {
			t.Fatalf("stat %s: %v", skillPath, skillErr)
		}
		if os.IsNotExist(questionsErr) {
			t.Fatalf("lore skill %s is missing questions.yaml", entry.Name())
		}
		if questionsErr != nil {
			t.Fatalf("stat %s: %v", questionsPath, questionsErr)
		}

		skillBytes, err := os.ReadFile(skillPath)
		if err != nil {
			t.Fatalf("read %s: %v", skillPath, err)
		}
		questions := loadQuestions(t, questionsPath)
		skills = append(skills, skillEval{
			name:        entry.Name(),
			instruction: string(skillBytes),
			questions:   questions,
		})
	}
	return skills
}

func loadQuestions(t *testing.T, path string) []question {
	t.Helper()

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}

	var questions []question
	if err := yaml.Unmarshal(data, &questions); err != nil {
		t.Fatalf("decode %s: %v", path, err)
	}
	seenIDs := make(map[string]struct{}, len(questions))
	for i, q := range questions {
		if q.ID == "" || q.Question == "" || q.Expected == "" {
			t.Fatalf("%s case %d is missing id, question, or expected", path, i+1)
		}
		if _, exists := seenIDs[q.ID]; exists {
			t.Fatalf("%s case %d duplicates id %q", path, i+1, q.ID)
		}
		seenIDs[q.ID] = struct{}{}
		if q.Polarity != "positive" && q.Polarity != "negative" {
			t.Fatalf("%s case %d has invalid polarity %q, want positive or negative", path, i+1, q.Polarity)
		}
	}
	if len(questions) == 0 {
		t.Fatalf("%s contains no questions", path)
	}
	return questions
}
