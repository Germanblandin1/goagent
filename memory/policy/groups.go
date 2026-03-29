package policy

import (
	"github.com/Germanblandin1/goagent"
)

// group represents an atomic sequence of messages that must be included or
// excluded together to preserve the tool call invariant.
// An assistant message with ToolCalls and all its subsequent RoleTool messages
// form an atomic group — they are never split.
type group struct {
	start int // inclusive index
	end   int // exclusive index — msgs[start:end]
}

// buildGroups groups messages respecting the tool call invariant.
// Complexity O(n): each message is visited exactly once.
func buildGroups(msgs []goagent.Message) []group {
	var groups []group
	i := 0
	for i < len(msgs) {
		g := group{start: i}
		i++
		if msgs[g.start].Role == goagent.RoleAssistant &&
			len(msgs[g.start].ToolCalls) > 0 {
			for i < len(msgs) && msgs[i].Role == goagent.RoleTool {
				i++
			}
		}
		g.end = i
		groups = append(groups, g)
	}
	return groups
}

// groupTokens estimates the token cost of all messages in a group using fn.
func groupTokens(g group, msgs []goagent.Message, fn TokenizerFunc) int {
	total := 0
	for _, msg := range msgs[g.start:g.end] {
		total += fn(msg)
	}
	return total
}
