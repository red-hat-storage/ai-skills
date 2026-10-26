package skills_test

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"maragu.dev/gai/eval"
	"sigs.k8s.io/yaml"
)

const (
	saturationThreshold = 0.05
	baseInstructions    = "Answer concisely. Use general product knowledge for general questions, but claim facts about a specific cluster only when the question provides evidence. If uncertain, say so instead of guessing."
)

type evalRecord struct {
	Name          string
	Group         string
	Sample        eval.Sample
	Results       []eval.Result
	Duration      string
	ResponseUsage *apiUsage `json:"response_usage,omitempty"`
	JudgeUsage    *apiUsage `json:"judge_usage,omitempty"`
}

type evalCondition struct {
	name         string
	instructions string
	withSkill    bool
}

type skillEvalRunner struct {
	repoRoot            string
	responseModel       string
	decisionModel       string
	client              skillEvalClient
	saturationSummaries []saturationSummary
	evalRecords         []evalRecord
	usage               tokenUsageSummary
}

type skillEvalClient interface {
	complete(ctx context.Context, instructions, input string) (string, *apiUsage, error)
	judge(ctx context.Context, question, expected, answer string) (decision, *apiUsage, error)
}

func (r *skillEvalRunner) run(t *testing.T, skills []skillEval) {
	for _, skill := range skills {
		r.runSkill(t, skill)
	}
}

func (r *skillEvalRunner) runSkill(t *testing.T, skill skillEval) {
	scoresByQuestion := make(map[string]pairedDecision, len(skill.questions))
	conditions := []evalCondition{
		{
			name:         "without_skill",
			instructions: baseInstructions,
		},
		{
			name:         "with_skill",
			instructions: baseInstructions + "\n\nSkill instructions:\n" + skill.instruction,
			withSkill:    true,
		},
	}

	eval.Run(t, skill.name, func(t *testing.T, _ *eval.E) {
		for _, q := range skill.questions {
			for _, condition := range conditions {
				eval.Run(t, condition.name+"/"+q.Polarity+"/"+q.ID, func(t *testing.T, _ *eval.E) {
					r.runCase(t, q, condition, scoresByQuestion)
				})
			}
		}
	})

	summary := skill.summarizeSaturation(scoresByQuestion)
	r.saturationSummaries = append(r.saturationSummaries, summary)
	status := "incomplete"
	if summary.Saturated != nil {
		status = "not_saturated"
		if *summary.Saturated {
			status = "saturated"
		}
	}
	fmt.Printf("skill=%s saturation=%s threshold=%.3f compared=%d/%d quality_without_skill=%s quality_with_skill=%s quality_delta=%s accuracy_delta=%s coverage_delta=%s unsupported_claims_delta=%s\n",
		summary.Skill, status, summary.Threshold, summary.ComparedQuestions, summary.QuestionCount,
		formatOptionalScore(summary.QualityWithoutSkill), formatOptionalScore(summary.QualityWithSkill),
		formatOptionalScore(summary.QualityDelta), formatOptionalScore(summary.AccuracyDelta),
		formatOptionalScore(summary.CoverageDelta), formatOptionalScore(summary.UnsupportedClaimsDelta),
	)
}

func (r *skillEvalRunner) runCase(t *testing.T, q question, condition evalCondition, scoresByQuestion map[string]pairedDecision) {
	started := time.Now()
	answer, responseUsage, err := r.client.complete(t.Context(), condition.instructions, q.Question)
	r.usage.Responses.add(responseUsage)
	if err != nil {
		t.Fatal(err)
	}

	sample := eval.NewTextSample(q.Question, q.Expected, answer)
	grade, judgeUsage, err := r.client.judge(t.Context(), q.Question, q.Expected, answer)
	r.usage.Decisions.add(judgeUsage)
	if err != nil {
		t.Fatal(err)
	}

	pair := scoresByQuestion[q.ID]
	if condition.withSkill {
		pair.withSkill = &grade
	} else {
		pair.withoutSkill = &grade
	}
	scoresByQuestion[q.ID] = pair

	r.evalRecords = append(r.evalRecords, evalRecord{
		Name:   t.Name(),
		Group:  "Skills",
		Sample: sample,
		Results: []eval.Result{
			{Type: "JudgeAccuracy", Score: grade.accuracy},
			{Type: "JudgeCoverage", Score: grade.coverage},
			{Type: "JudgeUnsupportedClaims", Score: grade.unsupportedClaims},
		},
		Duration:      time.Since(started).Round(time.Millisecond).String(),
		ResponseUsage: responseUsage,
		JudgeUsage:    judgeUsage,
	})
}

func (r *skillEvalRunner) writeResults(t *testing.T) {
	timestamp := time.Now().UTC()
	resultsName := "results-" + timestamp.Format("20060102T150405Z")
	var resultsPath string
	var resultsFile *os.File
	var err error
	for suffix := 0; ; suffix++ {
		filename := resultsName + ".yaml"
		if suffix > 0 {
			filename = fmt.Sprintf("%s-%d.yaml", resultsName, suffix+1)
		}
		resultsPath = filepath.Join(r.repoRoot, filename)
		resultsFile, err = os.OpenFile(resultsPath, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o666)
		if !os.IsExist(err) {
			break
		}
	}
	if err != nil {
		t.Errorf("create eval results: %v", err)
		return
	}

	metadata := map[string]string{
		"record_type":      "run_metadata",
		"timestamp":        timestamp.Format(time.RFC3339),
		"response_model":   r.responseModel,
		"reasoning_effort": reasoningEffort,
		"decision_model":   r.decisionModel,
	}
	records := make([]any, 0, len(r.evalRecords)+len(r.saturationSummaries)+2)
	records = append(records, metadata)
	for _, record := range r.evalRecords {
		records = append(records, record)
	}
	records = append(records, r.usage)
	for _, summary := range r.saturationSummaries {
		records = append(records, summary)
	}
	results, err := yaml.Marshal(records)
	if err != nil {
		t.Errorf("encode eval results: %v", err)
	} else if _, err := resultsFile.Write(results); err != nil {
		t.Errorf("write eval results: %v", err)
	}
	if err := resultsFile.Close(); err != nil {
		t.Errorf("close eval results: %v", err)
	}
	fmt.Printf("token_usage responses_input=%d responses_output=%d decisions_input=%d decisions_output=%d total=%d\n",
		r.usage.Responses.InputTokens, r.usage.Responses.OutputTokens,
		r.usage.Decisions.InputTokens, r.usage.Decisions.OutputTokens,
		r.usage.Responses.TotalTokens+r.usage.Decisions.TotalTokens,
	)
}

func formatOptionalScore(score *float64) string {
	if score == nil {
		return "n/a"
	}
	return fmt.Sprintf("%.3f", *score)
}
