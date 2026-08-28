package webhook

import "testing"

func TestRemoveBotMentions(t *testing.T) {
	tests := []struct {
		name    string
		message string
		botID   int
		want    string
	}{
		{
			name:    "leading Bitrix mention",
			message: "[USER=739515]Афина (dev)[/USER] перечисли имена руководителей маркетинга?",
			botID:   739515,
			want:    "перечисли имена руководителей маркетинга?",
		},
		{
			name:    "case insensitive tag",
			message: "[user=739515]Другое отображаемое имя[/UsEr]\nЧто такое МА?",
			botID:   739515,
			want:    "Что такое МА?",
		},
		{
			name:    "multiple bot mentions",
			message: "[USER=739515]Афина[/USER] вопрос [USER=739515]Афина[/USER]",
			botID:   739515,
			want:    "вопрос",
		},
		{
			name:    "other user mention remains",
			message: "[USER=739515]Афина[/USER] что назначено [USER=42]Ивану[/USER]?",
			botID:   739515,
			want:    "что назначено [USER=42]Ивану[/USER]?",
		},
		{
			name:    "message without mention",
			message: "  обычный вопрос  ",
			botID:   739515,
			want:    "обычный вопрос",
		},
		{
			name:    "only bot mention",
			message: " [USER=739515]Афина[/USER] ",
			botID:   739515,
			want:    "",
		},
		{
			name:    "malformed mention remains",
			message: "[USER=739515]Афина вопрос",
			botID:   739515,
			want:    "[USER=739515]Афина вопрос",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := removeBotMentions(test.message, test.botID); got != test.want {
				t.Fatalf("removeBotMentions() = %q, want %q", got, test.want)
			}
		})
	}
}
