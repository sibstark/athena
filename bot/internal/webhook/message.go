package webhook

import (
	"regexp"
	"strconv"
	"strings"
)

var userMentionPattern = regexp.MustCompile(`(?is)\[user=([0-9]+)\].*?\[/user\]`)

func removeBotMentions(message string, botID int) string {
	if botID <= 0 {
		return strings.TrimSpace(message)
	}

	cleaned := userMentionPattern.ReplaceAllStringFunc(message, func(mention string) string {
		matches := userMentionPattern.FindStringSubmatch(mention)
		mentionedID, err := strconv.Atoi(matches[1])
		if err != nil || mentionedID != botID {
			return mention
		}
		return ""
	})
	return strings.TrimSpace(cleaned)
}
