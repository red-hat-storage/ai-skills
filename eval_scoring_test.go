package skills_test

import "maragu.dev/gai/eval"

type pairedDecision struct {
	withoutSkill *decision
	withSkill    *decision
}

type saturationSummary struct {
	RecordType             string   `json:"record_type"`
	Skill                  string   `json:"skill"`
	Saturated              *bool    `json:"saturated,omitempty"`
	ComparedQuestions      int      `json:"compared_questions"`
	QuestionCount          int      `json:"question_count"`
	Threshold              float64  `json:"threshold"`
	QualityWithoutSkill    *float64 `json:"quality_without_skill,omitempty"`
	QualityWithSkill       *float64 `json:"quality_with_skill,omitempty"`
	QualityDelta           *float64 `json:"quality_delta,omitempty"`
	AccuracyDelta          *float64 `json:"accuracy_delta,omitempty"`
	CoverageDelta          *float64 `json:"coverage_delta,omitempty"`
	UnsupportedClaimsDelta *float64 `json:"unsupported_claims_delta,omitempty"`
}

type decision struct {
	accuracy          eval.Score
	coverage          eval.Score
	unsupportedClaims eval.Score
}

func (grade decision) quality() float64 {
	return (float64(grade.accuracy) + float64(grade.coverage) + (1 - float64(grade.unsupportedClaims))) / 3
}

func (skill skillEval) summarizeSaturation(scoresByQuestion map[string]pairedDecision) saturationSummary {
	summary := saturationSummary{
		RecordType:    "skill_saturation",
		Skill:         skill.name,
		QuestionCount: len(skill.questions),
		Threshold:     saturationThreshold,
	}
	var withoutAccuracy, withAccuracy float64
	var withoutCoverage, withCoverage float64
	var withoutUnsupported, withUnsupported float64
	var withoutQuality, withQuality float64
	for _, question := range skill.questions {
		pair := scoresByQuestion[question.ID]
		if pair.withoutSkill == nil || pair.withSkill == nil {
			continue
		}
		summary.ComparedQuestions++
		withoutAccuracy += float64(pair.withoutSkill.accuracy)
		withAccuracy += float64(pair.withSkill.accuracy)
		withoutCoverage += float64(pair.withoutSkill.coverage)
		withCoverage += float64(pair.withSkill.coverage)
		withoutUnsupported += float64(pair.withoutSkill.unsupportedClaims)
		withUnsupported += float64(pair.withSkill.unsupportedClaims)
		withoutQuality += pair.withoutSkill.quality()
		withQuality += pair.withSkill.quality()
	}
	if summary.ComparedQuestions == 0 {
		return summary
	}
	count := float64(summary.ComparedQuestions)
	withoutAccuracy /= count
	withAccuracy /= count
	withoutCoverage /= count
	withCoverage /= count
	withoutUnsupported /= count
	withUnsupported /= count
	withoutQuality /= count
	withQuality /= count

	accuracyDelta := withAccuracy - withoutAccuracy
	coverageDelta := withCoverage - withoutCoverage
	unsupportedDelta := withUnsupported - withoutUnsupported
	qualityDelta := withQuality - withoutQuality
	summary.QualityWithoutSkill = &withoutQuality
	summary.QualityWithSkill = &withQuality
	summary.QualityDelta = &qualityDelta
	summary.AccuracyDelta = &accuracyDelta
	summary.CoverageDelta = &coverageDelta
	summary.UnsupportedClaimsDelta = &unsupportedDelta
	if summary.ComparedQuestions != summary.QuestionCount {
		return summary
	}
	saturated := qualityDelta >= -saturationThreshold && qualityDelta <= saturationThreshold
	summary.Saturated = &saturated
	return summary
}
