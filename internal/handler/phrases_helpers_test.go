package handler

import (
	"testing"

	"autorepairos/internal/model"
)

func TestGeneratePhrasesHTML(t *testing.T) {
	got := generatePhrasesHTML(nil)
	if got == "" {
		t.Error("should not be empty")
	}
}

func TestGeneratePhraseEditForm(t *testing.T) {
	got := generatePhraseEditForm(model.TechnicianPhrase{ID: 1, Phrase: "test", Category: "cat", SortOrder: 5, Active: 1})
	if got == "" {
		t.Error("should not be empty")
	}
}
