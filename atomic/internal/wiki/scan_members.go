package wiki

import (
	"fmt"
	"os"
	"strings"
)

// ReadScanMembers lists a wiki's members without the filesystem walk a full
// Scan performs. An absent file or block returns (nil, nil), leaving the caller
// to decide whether that is fatal; only a real read failure errors.
func ReadScanMembers(indexPath string) ([]Member, error) {
	data, err := os.ReadFile(indexPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("wiki: read scan members: %w", err)
	}

	blockContent := extractBlockContent(string(data))
	if blockContent == "" {
		return nil, nil
	}

	var members []Member
	for _, line := range strings.Split(blockContent, "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "<repo ") {
			continue
		}
		path := attrValue(line, "path")
		status := attrValue(line, "status")
		summary := attrValue(line, "summary")
		signals := attrValue(line, "signals")
		if path == "" || status == "" {
			continue
		}
		m := Member{
			Path:   path,
			Status: status,
		}
		if summary != "" {
			m.SummaryPath = summary
		}
		if signals != "" {
			m.SignalsPath = signals
		}
		members = append(members, m)
	}
	return members, nil
}
