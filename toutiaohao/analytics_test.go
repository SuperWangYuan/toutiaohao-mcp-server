package toutiaohao

import (
	"testing"
)

func TestArticleStatsValidation_EmptyID(t *testing.T) {
	err := ValidateArticleStats("")
	if err == nil {
		t.Error("expected error for empty article_id")
	}
	if err.Error() != "article_id is required" {
		t.Errorf("error = %q, want %q", err.Error(), "article_id is required")
	}
}

func TestArticleStatsValidation_ValidID(t *testing.T) {
	err := ValidateArticleStats("12345")
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestReportTypeValidation_Default(t *testing.T) {
	rt := NormalizeReportType("")
	if rt != "weekly" {
		t.Errorf("NormalizeReportType(\"\") = %q, want weekly", rt)
	}
}

func TestReportTypeValidation_Valid(t *testing.T) {
	for _, rt := range []string{"daily", "weekly", "monthly"} {
		if err := ValidateReportType(rt); err != nil {
			t.Errorf("ValidateReportType(%q) = %v, want nil", rt, err)
		}
	}
}

func TestReportTypeValidation_Invalid(t *testing.T) {
	err := ValidateReportType("yearly")
	if err == nil {
		t.Error("expected error for invalid report type")
	}
}
